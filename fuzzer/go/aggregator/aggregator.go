package aggregator

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skia-dev/glog"
	"go.skia.org/infra/fuzzer/go/common"
	"go.skia.org/infra/fuzzer/go/config"
	"go.skia.org/infra/fuzzer/go/data"
	"go.skia.org/infra/fuzzer/go/deduplicator"
	"go.skia.org/infra/go/exec"
	"go.skia.org/infra/go/fileutil"
	"go.skia.org/infra/go/metrics2"
	"go.skia.org/infra/go/util"
	"golang.org/x/net/context"
	"google.golang.org/cloud/storage"
)

// Aggregator is a key part of the fuzzing operation
// (see https://skia.googlesource.com/buildbot/+/master/fuzzer/DESIGN.md).
// It will find new bad fuzzes generated by afl-fuzz and create the metadata required for them. It
// does this by searching in the specified AflOutputPath for new crashes and moves them to a
// temporary holding folder (specified by FuzzPath) for parsing, before sending them through the
// "aggregation pipeline".  This pipeline has three steps, Analysis, Upload and Bug Reporting.
// Analysis runs the fuzz against a debug and release version of Skia which produces stacktraces and
// error output.  Upload uploads these pieces to Google Storage (GCS).  Bug Reporting is used to
// either create or update a bug related to the given fuzz.
type Aggregator struct {
	// Should be set to true if a bug should be created for every bad fuzz found.
	// Example: Detecting regressions.
	// TODO(kjlubick): consider making this a function that clients supply so they can decide.
	MakeBugOnBadFuzz bool
	// Should be set if we want to upload grey fuzzes.  This should only be true
	// if we are changing versions.
	UploadGreyFuzzes bool

	storageClient *storage.Client

	// For passing the paths of new binaries that should be scanned.
	forAnalysis chan analysisPackage
	// For passing the file names of analyzed fuzzes that should be uploaded from where they rest on
	// disk in `fuzzPath`
	forUpload chan uploadPackage

	forBugReporting chan bugReportingPackage

	// maps category to its deduplicator
	deduplicators map[string]*deduplicator.Deduplicator

	// The shutdown channels are used to signal shutdowns.  There are two groups, to
	// allow for a softer, cleaner shutdown w/ minimal lost work.
	// Group A (monitoring) includes the scanning and the monitoring routine.
	// Group B (aggregation) include the analysis and upload routines and the bug reporting routine.
	monitoringShutdown   chan bool
	monitoringWaitGroup  *sync.WaitGroup
	aggregationShutdown  chan bool
	aggregationWaitGroup *sync.WaitGroup
	// These three counts are used to determine if there is any pending work.
	// There is no pending work if all three of these values are equal and the
	// work queues are empty.
	analysisCount  int64
	uploadCount    int64
	bugReportCount int64

	greyNames []string
	badNames  []string
}

const (
	BAD_FUZZ       = "bad"
	GREY_FUZZ      = "grey"
	HANG_THRESHOLD = 10
)

var (
	CLANG_DEBUG   = common.TEST_HARNESS_NAME + "_clang_debug"
	CLANG_RELEASE = common.TEST_HARNESS_NAME + "_clang_release"
	ASAN_DEBUG    = common.TEST_HARNESS_NAME + "_asan_debug"
	ASAN_RELEASE  = common.TEST_HARNESS_NAME + "_asan_release"
)

// analysisPackage is a struct containing all the pieces of a fuzz needed to analyse it.
type analysisPackage struct {
	FilePath string
	Category string
}

// uploadPackage is a struct containing all the pieces of a fuzz that need to be uploaded to GCS
type uploadPackage struct {
	Data     data.GCSPackage
	FilePath string
	// Must be BAD_FUZZ or GREY_FUZZ
	FuzzType string
	Category string
}

// bugReportingPackage is a struct containing the pieces of a fuzz that may need to have
// a bug filed or updated.
type bugReportingPackage struct {
	FuzzName   string
	CommitHash string
	IsBadFuzz  bool
	Category   string
}

// StartAggregator creates and starts a Aggregator.
// If there is a problem starting up, an error is returned.  Other errors will be logged.
func StartAggregator(s *storage.Client, startingReports map[string]<-chan data.FuzzReport) (*Aggregator, error) {
	b := Aggregator{
		storageClient:      s,
		forAnalysis:        make(chan analysisPackage, 10000),
		forUpload:          make(chan uploadPackage, 100),
		forBugReporting:    make(chan bugReportingPackage, 100),
		MakeBugOnBadFuzz:   false,
		UploadGreyFuzzes:   false,
		deduplicators:      make(map[string]*deduplicator.Deduplicator),
		monitoringShutdown: make(chan bool, 2),
		// aggregationShutdown needs to be created with a calculated capacity in start
	}

	// preload the deduplicator
	for _, category := range config.Generator.FuzzesToGenerate {
		d := deduplicator.New()
		for report := range startingReports[category] {
			d.IsUnique(report)
		}
		b.deduplicators[category] = d
	}

	return &b, b.start()
}

// start starts up the Aggregator.  It refreshes all status it needs and builds a debug and a
// release version of Skia for use in analysis.  It then spawns the aggregation pipeline and a
// monitoring thread.
func (agg *Aggregator) start() error {
	// Set the wait groups to fresh
	agg.monitoringWaitGroup = &sync.WaitGroup{}
	agg.aggregationWaitGroup = &sync.WaitGroup{}
	atomic.StoreInt64(&agg.analysisCount, int64(0))
	atomic.StoreInt64(&agg.uploadCount, int64(0))
	atomic.StoreInt64(&agg.bugReportCount, int64(0))

	if err := agg.buildAnalysisBinaries(); err != nil {
		return err
	}

	agg.monitoringWaitGroup.Add(1)
	go agg.scanForNewCandidates()

	numAnalysisProcesses := config.Aggregator.NumAnalysisProcesses
	if numAnalysisProcesses <= 0 {
		// TODO(kjlubick): Actually make this smart based on the number of cores
		numAnalysisProcesses = 20
	}
	for i := 0; i < numAnalysisProcesses; i++ {
		agg.aggregationWaitGroup.Add(1)
		go agg.waitForAnalysis(i)
	}

	numUploadProcesses := config.Aggregator.NumUploadProcesses
	if numUploadProcesses <= 0 {
		// TODO(kjlubick): Actually make this smart based on the number of cores/number
		// of aggregation processes
		numUploadProcesses = 5
	}
	for i := 0; i < numUploadProcesses; i++ {
		agg.aggregationWaitGroup.Add(1)
		go agg.waitForUploads(i)
	}
	agg.aggregationWaitGroup.Add(1)
	go agg.waitForBugReporting()
	agg.aggregationShutdown = make(chan bool, numAnalysisProcesses+numUploadProcesses+1)
	// start background routine to monitor queue details
	agg.monitoringWaitGroup.Add(1)
	go agg.monitorStatus(numAnalysisProcesses, numUploadProcesses)
	return nil
}

// buildAnalysisBinaries creates the 4 executables we need to perform analysis and makes a copy of
// them in the executablePath.  We need (Debug,Release) x (Clang,ASAN).  The copied binaries have
// a suffix like _clang_debug
func (agg *Aggregator) buildAnalysisBinaries() error {
	if _, err := fileutil.EnsureDirExists(config.Aggregator.FuzzPath); err != nil {
		return err
	}
	if _, err := fileutil.EnsureDirExists(config.Aggregator.ExecutablePath); err != nil {
		return err
	}
	if err := common.BuildClangHarness("Debug", true); err != nil {
		return err
	}
	outPath := filepath.Join(config.Generator.SkiaRoot, "out")
	if err := fileutil.CopyExecutable(filepath.Join(outPath, "Debug", common.TEST_HARNESS_NAME), filepath.Join(config.Aggregator.ExecutablePath, CLANG_DEBUG)); err != nil {
		return err
	}
	if err := common.BuildClangHarness("Release", true); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(outPath, "Release", common.TEST_HARNESS_NAME), filepath.Join(config.Aggregator.ExecutablePath, CLANG_RELEASE)); err != nil {
		return err
	}
	if err := common.BuildASANHarness("Debug", false); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(outPath, "Debug", common.TEST_HARNESS_NAME), filepath.Join(config.Aggregator.ExecutablePath, ASAN_DEBUG)); err != nil {
		return err
	}
	if err := common.BuildASANHarness("Release", false); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(outPath, "Release", common.TEST_HARNESS_NAME), filepath.Join(config.Aggregator.ExecutablePath, ASAN_RELEASE)); err != nil {
		return err
	}
	return nil
}

// scanForNewCandidates runs scanHelper once every config.Aggregator.RescanPeriod, which scans the
// config.Generator.AflOutputPath for new fuzzes.  If scanHelper returns an error, this method
// will terminate.
func (agg *Aggregator) scanForNewCandidates() {
	defer agg.monitoringWaitGroup.Done()

	alreadyFoundFuzzes := &SortedStringSlice{}
	// time.Tick does not fire immediately, so we fire it manually once.
	if err := agg.scanHelper(alreadyFoundFuzzes); err != nil {
		glog.Errorf("Scanner terminated due to error: %v", err)
		return
	}
	glog.Infof("Sleeping for %s, then waking up to find new crashes again", config.Aggregator.RescanPeriod)

	t := time.Tick(config.Aggregator.RescanPeriod)
	for {
		select {
		case <-t:
			if err := agg.scanHelper(alreadyFoundFuzzes); err != nil {
				glog.Errorf("Aggregator scanner terminated due to error: %s", err)
				return
			}
			glog.Infof("Sleeping for %s, then waking up to find new crashes again", config.Aggregator.RescanPeriod)
		case <-agg.monitoringShutdown:
			glog.Info("Aggregator scanner got signal to shut down")
			return
		}

	}
}

// scanHelper runs findBadFuzzPaths for every category, logs the output and keeps
// alreadyFoundFuzzes up to date.
func (agg *Aggregator) scanHelper(alreadyFoundFuzzes *SortedStringSlice) error {
	for _, category := range config.Generator.FuzzesToGenerate {
		newlyFound, err := findBadFuzzPaths(category, alreadyFoundFuzzes)
		if err != nil {
			return err
		}
		// AFL-fuzz does not write crashes or hangs atomically, so this workaround waits for a bit after
		// we have references to where the crashes will be.
		// TODO(kjlubick), switch to using flock once afl-fuzz implements that upstream.
		time.Sleep(time.Second)
		metrics2.GetInt64Metric("fuzzer.fuzzes.newly-found", map[string]string{"category": category}).Update(int64(len(newlyFound)))
		glog.Infof("%d newly found %s bad fuzzes", len(newlyFound), category)
		for _, f := range newlyFound {
			agg.forAnalysis <- analysisPackage{
				FilePath: f,
				Category: category,
			}
		}
		alreadyFoundFuzzes.Append(newlyFound)
	}

	return nil
}

// findBadFuzzPaths looks through all the afl-fuzz directories contained in the passed in path and
// returns the path to all files that are in a crash* folder that are not already in
// 'alreadyFoundFuzzes'.  It also sends them to the forAnalysis channel when it finds them.
// The output from afl-fuzz looks like:
// afl_output_path/category/
//		-fuzzer0/
//			-crashes/  <-- bad fuzzes end up here
//			-hangs/
//			-queue/
//			-fuzzer_stats
//		-fuzzer1/
//		...
func findBadFuzzPaths(category string, alreadyFoundFuzzes *SortedStringSlice) ([]string, error) {
	badFuzzPaths := make([]string, 0)

	scanPath := filepath.Join(config.Generator.AflOutputPath, category)
	aflDir, err := os.Open(scanPath)
	if os.IsNotExist(err) {
		glog.Warningf("Path to scan %s does not exist.  Returning 0 found fuzzes", scanPath)
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer util.Close(aflDir)

	fuzzerFolders, err := aflDir.Readdir(-1)
	if err != nil {
		return nil, err
	}

	for _, fuzzerFolderInfo := range fuzzerFolders {
		// fuzzerFolderName an os.FileInfo like fuzzer0, fuzzer1
		path := filepath.Join(scanPath, fuzzerFolderInfo.Name())
		fuzzerDir, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer util.Close(fuzzerDir)

		fuzzerContents, err := fuzzerDir.Readdir(-1)
		if err != nil {
			return nil, err
		}
		for _, info := range fuzzerContents {
			// Look through fuzzerN/crashes
			if info.IsDir() && strings.HasPrefix(info.Name(), "crashes") {
				crashPath := filepath.Join(path, info.Name())
				crashDir, err := os.Open(crashPath)
				if err != nil {
					return nil, err
				}
				defer util.Close(crashDir)

				crashContents, err := crashDir.Readdir(-1)
				if err != nil {
					return nil, err
				}
				for _, crash := range crashContents {
					// Make sure the files are actually crashable files we haven't found before
					if crash.Name() != "README.txt" {
						if fuzzPath := filepath.Join(crashPath, crash.Name()); !alreadyFoundFuzzes.Contains(fuzzPath) {
							badFuzzPaths = append(badFuzzPaths, fuzzPath)
						}
					}
				}
			}
		}
	}
	return badFuzzPaths, nil
}

// waitForAnalysis waits for files that need to be analyzed (from forAnalysis) and makes a copy of
// them in agg.fuzzPath with their hash as a file name. It then analyzes it using the supplied
// AnalysisPackage and then signals the results should be uploaded. If any unrecoverable errors
// happen, this method terminates.
func (agg *Aggregator) waitForAnalysis(identifier int) {
	defer agg.aggregationWaitGroup.Done()
	defer metrics2.NewCounter("analysis-process-count", nil).Dec(int64(1))
	glog.Infof("Spawning analyzer %d", identifier)

	// our own unique working folder
	executableDir := filepath.Join(config.Aggregator.ExecutablePath, fmt.Sprintf("analyzer%d", identifier))
	if err := setupAnalysis(executableDir); err != nil {
		glog.Errorf("Analyzer %d terminated due to error: %s", identifier, err)
		return
	}
	for {
		select {
		case badFuzz := <-agg.forAnalysis:
			atomic.AddInt64(&agg.analysisCount, int64(1))
			err := agg.analysisHelper(executableDir, badFuzz)
			if err != nil {
				atomic.AddInt64(&agg.analysisCount, int64(-1))
				glog.Errorf("Analyzer %d terminated due to error: %s", identifier, err)
				return
			}
		case <-agg.aggregationShutdown:
			glog.Infof("Analyzer %d recieved shutdown signal", identifier)
			return
		}
	}
}

// analysisHelper performs the analysis on the given fuzz and returns an error if anything goes
// wrong.  On success, the results will be placed in the upload queue.
func (agg *Aggregator) analysisHelper(executableDir string, badFuzz analysisPackage) error {
	hash, data, err := calculateHash(badFuzz.FilePath)
	if err != nil {
		return err
	}
	newFuzzPath := filepath.Join(config.Aggregator.FuzzPath, hash)
	if err := ioutil.WriteFile(newFuzzPath, data, 0644); err != nil {
		return err
	}
	if upload, err := analyze(executableDir, hash, badFuzz.Category); err != nil {
		return fmt.Errorf("Problem analyzing %s, terminating: %s", newFuzzPath, err)
	} else {
		agg.forUpload <- upload
	}
	return nil
}

// Setup cleans out the working directory and makes a copy of the Debug and Release fuzz executable
// in that directory.
func setupAnalysis(workingDirPath string) error {
	// Delete all previous executables to get a clean start
	if err := os.RemoveAll(workingDirPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(workingDirPath, 0755); err != nil {
		return err
	}

	// make a copy of the 4 executables that were made in buildAnalysisBinaries()
	if err := fileutil.CopyExecutable(filepath.Join(config.Aggregator.ExecutablePath, CLANG_DEBUG), filepath.Join(workingDirPath, CLANG_DEBUG)); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(config.Aggregator.ExecutablePath, CLANG_RELEASE), filepath.Join(workingDirPath, CLANG_RELEASE)); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(config.Aggregator.ExecutablePath, ASAN_DEBUG), filepath.Join(workingDirPath, ASAN_DEBUG)); err != nil {
		return err
	}
	if err := fileutil.CopyExecutable(filepath.Join(config.Aggregator.ExecutablePath, ASAN_RELEASE), filepath.Join(workingDirPath, ASAN_RELEASE)); err != nil {
		return err
	}
	return nil
}

// analyze simply invokes performAnalysis with a fuzz under both the Debug and Release build.  Upon
// completion, it checks to see if the fuzz is a grey fuzz and sets the FuzzType accordingly.
func analyze(workingDirPath, filename, category string) (uploadPackage, error) {
	upload := uploadPackage{
		Data: data.GCSPackage{
			Name:         filename,
			FuzzCategory: category,
		},
		FuzzType: BAD_FUZZ,
		FilePath: filepath.Join(config.Aggregator.FuzzPath, filename),
		Category: category,
	}

	if dump, stderr, err := performAnalysis(workingDirPath, CLANG_DEBUG, upload.FilePath, category); err != nil {
		return upload, err
	} else {
		upload.Data.Debug.Dump = dump
		upload.Data.Debug.StdErr = stderr
	}
	if dump, stderr, err := performAnalysis(workingDirPath, CLANG_RELEASE, upload.FilePath, category); err != nil {
		return upload, err
	} else {
		upload.Data.Release.Dump = dump
		upload.Data.Release.StdErr = stderr
	}
	// AddressSanitizer only outputs to stderr
	if _, stderr, err := performAnalysis(workingDirPath, ASAN_DEBUG, upload.FilePath, category); err != nil {
		return upload, err
	} else {
		upload.Data.Debug.Asan = stderr
	}
	if _, stderr, err := performAnalysis(workingDirPath, ASAN_RELEASE, upload.FilePath, category); err != nil {
		return upload, err
	} else {
		upload.Data.Release.Asan = stderr
	}
	if r := data.ParseGCSPackage(upload.Data); r.Debug.Flags == data.TerminatedGracefully && r.Release.Flags == data.TerminatedGracefully {
		upload.FuzzType = GREY_FUZZ
	}
	return upload, nil
}

// performAnalysis executes a command from the working dir specified using
// AnalysisArgs for a given fuzz category. The crash dumps (which
// come via standard out) and standard errors are recorded as strings.
func performAnalysis(workingDirPath, executableName, pathToFile, category string) (string, string, error) {

	var dump bytes.Buffer
	var stdErr bytes.Buffer

	// GNU timeout is used instead of the option on exec.Command because experimentation with the
	// latter showed evidence of that way leaking processes, which led to OOM errors.
	cmd := &exec.Command{
		Name:        "timeout",
		Args:        common.AnalysisArgsFor(category, "./"+executableName, pathToFile),
		LogStdout:   false,
		LogStderr:   false,
		Stdout:      &dump,
		Stderr:      &stdErr,
		Dir:         workingDirPath,
		InheritPath: true,
		Env:         []string{common.ASAN_OPTIONS},
	}

	//errors are fine/expected from this, as we are dealing with bad fuzzes
	if err := exec.Run(cmd); err != nil {
		return dump.String(), stdErr.String(), nil
	}
	return dump.String(), stdErr.String(), nil
}

// calcuateHash calculates the sha1 hash of a file, given its path.  It returns both the hash as a
// hex-encoded string and the contents of the file.
func calculateHash(path string) (hash string, data []byte, err error) {
	data, err = ioutil.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("Problem reading file for hashing %s: %s", path, err)
	}
	return fmt.Sprintf("%x", sha1.Sum(data)), data, nil
}

// A SortedStringSlice has a sortable string slice which is always kept sorted. This allows for an
// implementation of Contains that runs in O(log n)
type SortedStringSlice struct {
	strings sort.StringSlice
}

// Contains returns true if the passed in string is in the underlying slice
func (s *SortedStringSlice) Contains(str string) bool {
	i := s.strings.Search(str)
	if i < len(s.strings) && s.strings[i] == str {
		return true
	}
	return false
}

// Append adds all of the strings to the underlying slice and sorts it
func (s *SortedStringSlice) Append(strs []string) {
	s.strings = append(s.strings, strs...)
	s.strings.Sort()
}

// waitForUploads waits for uploadPackages to be sent through the forUpload channel and then uploads
// them.  If any unrecoverable errors happen, this method terminates.
func (agg *Aggregator) waitForUploads(identifier int) {
	defer agg.aggregationWaitGroup.Done()
	defer metrics2.NewCounter("upload-process-count", nil).Dec(int64(1))
	glog.Infof("Spawning uploader %d", identifier)
	for {
		select {
		case p := <-agg.forUpload:
			atomic.AddInt64(&agg.uploadCount, int64(1))
			if !agg.UploadGreyFuzzes && p.FuzzType == GREY_FUZZ {
				glog.Infof("Skipping upload of grey fuzz %s", p.Data.Name)
				// We are skipping the bugReport, so increment the counts.
				atomic.AddInt64(&agg.bugReportCount, int64(1))
				continue
			}
			d, found := agg.deduplicators[p.Category]
			if !found {
				glog.Errorf("Problem in Uploader %d, no deduplicator found for category %q; %#v;", identifier, p.Category, agg.deduplicators)
				return
			}
			if p.FuzzType != GREY_FUZZ && !d.IsUnique(data.ParseReport(p.Data)) {
				glog.Infof("Skipping upload of duplicate fuzz %s", p.Data.Name)
				// We are skipping the bugReport, so increment the counts.
				atomic.AddInt64(&agg.bugReportCount, int64(1))
				continue
			}
			if err := agg.upload(p); err != nil {
				glog.Errorf("Uploader %d terminated due to error: %s", identifier, err)
				// We are skipping the bugReport, so increment the counts.
				atomic.AddInt64(&agg.bugReportCount, int64(1))
				return
			}
			agg.forBugReporting <- bugReportingPackage{
				FuzzName:   p.Data.Name,
				CommitHash: config.Generator.SkiaVersion.Hash,
				IsBadFuzz:  p.FuzzType == BAD_FUZZ,
			}
		case <-agg.aggregationShutdown:
			glog.Infof("Uploader %d recieved shutdown signal", identifier)
			return
		}
	}
}

// upload breaks apart the uploadPackage into its constituant parts and uploads them to GCS using
// some helper methods.
func (agg *Aggregator) upload(p uploadPackage) error {
	glog.Infof("uploading %s with file %s and analysis bytes %d;%d;%d|%d;%d;%d", p.Data.Name, p.FilePath, len(p.Data.Debug.Asan), len(p.Data.Debug.Dump), len(p.Data.Debug.StdErr), len(p.Data.Release.Asan), len(p.Data.Release.Dump), len(p.Data.Release.StdErr))
	if p.FuzzType == GREY_FUZZ {
		agg.greyNames = append(agg.greyNames, p.Data.Name)
	} else {
		agg.badNames = append(agg.badNames, p.Data.Name)
	}

	if err := agg.uploadBinaryFromDisk(p, p.Data.Name, p.FilePath); err != nil {
		return err
	}
	if err := agg.uploadString(p, p.Data.Name+"_debug.asan", p.Data.Debug.Asan); err != nil {
		return err
	}
	if err := agg.uploadString(p, p.Data.Name+"_debug.dump", p.Data.Debug.Dump); err != nil {
		return err
	}
	if err := agg.uploadString(p, p.Data.Name+"_debug.err", p.Data.Debug.StdErr); err != nil {
		return err
	}
	if err := agg.uploadString(p, p.Data.Name+"_release.asan", p.Data.Release.Asan); err != nil {
		return err
	}
	if err := agg.uploadString(p, p.Data.Name+"_release.dump", p.Data.Release.Dump); err != nil {
		return err
	}
	return agg.uploadString(p, p.Data.Name+"_release.err", p.Data.Release.StdErr)
}

// uploadBinaryFromDisk uploads a binary file on disk to GCS, returning an error if anything
// goes wrong.
func (agg *Aggregator) uploadBinaryFromDisk(p uploadPackage, fileName, filePath string) error {
	name := fmt.Sprintf("%s/%s/%s/%s/%s", p.Category, config.Generator.SkiaVersion.Hash, p.FuzzType, p.Data.Name, fileName)
	w := agg.storageClient.Bucket(config.GS.Bucket).Object(name).NewWriter(context.Background())
	defer util.Close(w)
	// We set the encoding to avoid accidental crashes if Chrome were to try to render a fuzzed png
	// or svg or something.
	w.ObjectAttrs.ContentEncoding = "application/octet-stream"

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("There was a problem reading %s for uploading : %s", filePath, err)
	}

	if n, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("There was a problem uploading binary file %s.  Only uploaded %d bytes : %s", name, n, err)
	}
	return nil
}

// uploadBinaryFromDisk uploads the contents of a string as a file to GCS, returning an error if
// anything goes wrong.
func (agg *Aggregator) uploadString(p uploadPackage, fileName, contents string) error {
	name := fmt.Sprintf("%s/%s/%s/%s/%s", p.Category, config.Generator.SkiaVersion.Hash, p.FuzzType, p.Data.Name, fileName)
	w := agg.storageClient.Bucket(config.GS.Bucket).Object(name).NewWriter(context.Background())
	defer util.Close(w)
	w.ObjectAttrs.ContentEncoding = "text/plain"

	if n, err := w.Write([]byte(contents)); err != nil {
		return fmt.Errorf("There was a problem uploading %s.  Only uploaded %d bytes: %s", name, n, err)
	}
	return nil
}

// waitForUploads waits for uploadPackages to be sent through the forUpload channel and then uploads
// them.  If any unrecoverable errors happen, this method terminates.
func (agg *Aggregator) waitForBugReporting() {
	defer agg.aggregationWaitGroup.Done()
	glog.Info("Spawning bug reporting routine")
	for {
		select {
		case p := <-agg.forBugReporting:
			if err := agg.bugReportingHelper(p); err != nil {
				glog.Errorf("Bug reporting terminated due to error: %s", err)
				return
			}
		case <-agg.aggregationShutdown:
			glog.Infof("Bug reporting routine recieved shutdown signal")
			return
		}
	}
}

// bugReportingHelper is a helper function to report bugs if the aggregator is configured to.
func (agg *Aggregator) bugReportingHelper(p bugReportingPackage) error {
	defer atomic.AddInt64(&agg.bugReportCount, int64(1))
	if agg.MakeBugOnBadFuzz && p.IsBadFuzz {
		glog.Warningf("Should create bug for %s", p.FuzzName)
	}
	return nil
}

// monitorStatus sets up the monitoring routine, which reports how big the work queues are and how
// many processes are up.
func (agg *Aggregator) monitorStatus(numAnalysisProcesses, numUploadProcesses int) {
	defer agg.monitoringWaitGroup.Done()
	analysisProcessCount := metrics2.NewCounter("analysis-process-count", nil)
	analysisProcessCount.Reset()
	analysisProcessCount.Inc(int64(numAnalysisProcesses))
	uploadProcessCount := metrics2.NewCounter("upload-process-count", nil)
	uploadProcessCount.Reset()
	uploadProcessCount.Inc(int64(numUploadProcesses))

	t := time.Tick(config.Aggregator.StatusPeriod)
	for {
		select {
		case <-agg.monitoringShutdown:
			glog.Info("aggregator monitor got signal to shut down")
			return
		case <-t:
			metrics2.GetInt64Metric("fuzzer.queue-size.analysis", nil).Update(int64(len(agg.forAnalysis)))
			metrics2.GetInt64Metric("fuzzer.queue-size.upload", nil).Update(int64(len(agg.forUpload)))
			metrics2.GetInt64Metric("fuzzer.queue-size.bug-report", nil).Update(int64(len(agg.forBugReporting)))
		}
	}
}

// Shutdown gracefully shuts down the aggregator. Anything that was being processed will finish
// prior to the shutdown.
func (agg *Aggregator) ShutDown() {
	// once for the monitoring and once for the scanning routines
	agg.monitoringShutdown <- true
	agg.monitoringShutdown <- true
	agg.monitoringWaitGroup.Wait()
	// wait for everything to finish analysis and upload
	agg.WaitForEmptyQueues()

	// signal once for every group b thread we started, which is the capacity of our
	// aggregationShutdown channel.
	for i := len(agg.aggregationShutdown); i < cap(agg.aggregationShutdown); i++ {
		agg.aggregationShutdown <- true
	}
	agg.aggregationWaitGroup.Wait()
}

// RestartAnalysis restarts the shut down aggregator.  Anything that is in the scanning directory
// should be cleared out, lest it be rescanned/analyzed.
func (agg *Aggregator) RestartAnalysis() error {
	for _, d := range agg.deduplicators {
		d.Clear()
	}
	return agg.start()
}

// WaitForEmptyQueues will return once there is nothing more in the analysis-upload-report
// pipeline, waiting in increments of config.Aggregator.StatusPeriod until it is done.
func (agg *Aggregator) WaitForEmptyQueues() {
	a := len(agg.forAnalysis)
	u := len(agg.forUpload)
	b := len(agg.forBugReporting)
	if a == 0 && u == 0 && b == 0 && agg.analysisCount == agg.uploadCount && agg.uploadCount == agg.bugReportCount {
		glog.Info("Queues were already empty")
		return
	}
	t := time.Tick(config.Aggregator.StatusPeriod)
	glog.Infof("Waiting %s for the aggregator's queues to be empty", config.Aggregator.StatusPeriod)
	hangCount := 0
	for _ = range t {
		a = len(agg.forAnalysis)
		u = len(agg.forUpload)
		b = len(agg.forBugReporting)
		glog.Infof("AnalysisQueue: %d, UploadQueue: %d, BugReportingQueue: %d", a, u, b)
		glog.Infof("AnalysisTotal: %d, UploadTotal: %d, BugReportingTotal: %d", agg.analysisCount, agg.uploadCount, agg.bugReportCount)
		if a == 0 && u == 0 && b == 0 && agg.analysisCount == agg.uploadCount && agg.uploadCount == agg.bugReportCount {
			break
		}
		// This prevents waiting forever if an upload crashes, aborts or otherwise hangs.
		hangCount++
		if hangCount >= HANG_THRESHOLD {
			glog.Warningf("Was waiting for %d rounds and still wasn't done.  Quitting anyway.", hangCount)
		}

		glog.Infof("Waiting %s for the aggregator's queues to be empty", config.Aggregator.StatusPeriod)
	}
	atomic.StoreInt64(&agg.analysisCount, int64(0))
	atomic.StoreInt64(&agg.uploadCount, int64(0))
	atomic.StoreInt64(&agg.bugReportCount, int64(0))
}

// ForceAnalysis directly adds the given path to the analysis queue, where it will be analyzed,
// uploaded and possibly bug reported.
func (agg *Aggregator) ForceAnalysis(path, category string) {
	agg.forAnalysis <- analysisPackage{
		FilePath: path,
		Category: category,
	}
}

func (agg *Aggregator) ClearUploadedFuzzNames() {
	agg.greyNames = []string{}
	agg.badNames = []string{}
}

func (agg *Aggregator) UploadedFuzzNames() (bad, grey []string) {
	return agg.badNames, agg.greyNames
}
