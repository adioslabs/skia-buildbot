package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.skia.org/infra/ct/go/ctfe/admin_tasks"
	"go.skia.org/infra/ct/go/ctfe/capture_skps"
	"go.skia.org/infra/ct/go/ctfe/chromium_builds"
	"go.skia.org/infra/ct/go/ctfe/chromium_perf"
	"go.skia.org/infra/ct/go/ctfe/lua_scripts"
	"go.skia.org/infra/ct/go/ctfe/task_common"
	ctfeutil "go.skia.org/infra/ct/go/ctfe/util"
	"go.skia.org/infra/ct/go/frontend"
	"go.skia.org/infra/go/exec"
	skutil "go.skia.org/infra/go/util"

	expect "github.com/stretchr/testify/assert"
	assert "github.com/stretchr/testify/require"
)

// CommonCols without TsStarted or TsCompleted set.
func pendingCommonCols() task_common.CommonCols {
	return task_common.CommonCols{
		Id:       42,
		TsAdded:  sql.NullInt64{Int64: 20080726180513, Valid: true},
		Username: "nobody@chromium.org",
	}
}

// Given a command generated by one of the Execute methods in main.go, extracts the value of the
// run_id command-line flag. If not found, signals a test failure.
func getRunId(t *testing.T, cmd *exec.Command) string {
	regexp := regexp.MustCompile("^--run_id=(.*)$")
	for _, arg := range cmd.Args {
		match := regexp.FindStringSubmatch(arg)
		if match != nil && len(match) >= 2 {
			return match[1]
		}
	}
	assert.Contains(t, strings.Join(cmd.Args, " "), "--run_id=")
	assert.Nil(t, "getRunId is broken")
	return ""
}

// Checks that the contents of filepath are expected; otherwise signals a test failure.
func assertFileContents(t *testing.T, filepath, expected string) {
	actual, err := ioutil.ReadFile(filepath)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(actual))
}

func pendingChromiumPerfTask() ChromiumPerfTask {
	return ChromiumPerfTask{
		DBTask: chromium_perf.DBTask{
			CommonCols:           pendingCommonCols(),
			Benchmark:            "benchmark",
			Platform:             "Linux",
			PageSets:             "All",
			RepeatRuns:           1,
			BenchmarkArgs:        "benchmarkargs",
			BrowserArgsNoPatch:   "banp",
			BrowserArgsWithPatch: "bawp",
			Description:          "description",
			ChromiumPatch:        "chromiumpatch",
			BlinkPatch:           "blinkpatch",
			SkiaPatch:            "skiapatch",
		},
	}
}

func TestChromiumPerfExecute(t *testing.T) {
	task := pendingChromiumPerfTask()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	mockRun.SetDelegateRun(func(cmd *exec.Command) error {
		runId := getRunId(t, cmd)
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".chromium.patch"),
			"chromiumpatch\n")
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".blink.patch"),
			"blinkpatch\n")
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".skia.patch"),
			"skiapatch\n")
		return nil
	})
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "run_chromium_perf_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--description=description")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--benchmark_name=benchmark")
	expect.Contains(t, cmd.Args, "--target_platform=Linux")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	expect.Contains(t, cmd.Args, "--repeat_benchmark=1")
	expect.Contains(t, cmd.Args, "--benchmark_extra_args=benchmarkargs")
	expect.Contains(t, cmd.Args, "--browser_extra_args_nopatch=banp")
	expect.Contains(t, cmd.Args, "--browser_extra_args_withpatch=bawp")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func pendingCaptureSkpsTask() CaptureSkpsTask {
	return CaptureSkpsTask{
		DBTask: capture_skps.DBTask{
			CommonCols:  pendingCommonCols(),
			PageSets:    "All",
			ChromiumRev: "c14d891d44f0afff64e56ed7c9702df1d807b1ee",
			SkiaRev:     "586101c79b0490b50623e76c71a5fd67d8d92b08",
			Description: "description",
		},
	}
}

func TestCaptureSkpsExecute(t *testing.T) {
	task := pendingCaptureSkpsTask()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "capture_skps_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--description=description")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	expect.Contains(t, cmd.Args, "--chromium_build=c14d891-586101c")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func pendingLuaScriptTaskWithAggregator() LuaScriptTask {
	return LuaScriptTask{
		DBTask: lua_scripts.DBTask{
			CommonCols:          pendingCommonCols(),
			PageSets:            "All",
			ChromiumRev:         "c14d891d44f0afff64e56ed7c9702df1d807b1ee",
			SkiaRev:             "586101c79b0490b50623e76c71a5fd67d8d92b08",
			LuaScript:           `print("lualualua")`,
			LuaAggregatorScript: `print("aaallluuu")`,
			Description:         "description",
		},
	}
}

func TestLuaScriptExecuteWithAggregator(t *testing.T) {
	task := pendingLuaScriptTaskWithAggregator()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	mockRun.SetDelegateRun(func(cmd *exec.Command) error {
		runId := getRunId(t, cmd)
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".lua"),
			`print("lualualua")`)
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".aggregator"),
			`print("aaallluuu")`)
		return nil
	})
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "run_lua_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--description=description")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	expect.Contains(t, cmd.Args, "--chromium_build=c14d891-586101c")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func TestLuaScriptExecuteWithoutAggregator(t *testing.T) {
	task := LuaScriptTask{
		DBTask: lua_scripts.DBTask{
			CommonCols:          pendingCommonCols(),
			PageSets:            "All",
			ChromiumRev:         "c14d891d44f0afff64e56ed7c9702df1d807b1ee",
			SkiaRev:             "586101c79b0490b50623e76c71a5fd67d8d92b08",
			LuaScript:           `print("lualualua")`,
			LuaAggregatorScript: "",
			Description:         "description",
		},
	}
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	mockRun.SetDelegateRun(func(cmd *exec.Command) error {
		runId := getRunId(t, cmd)
		assertFileContents(t, filepath.Join(os.TempDir(), runId+".lua"),
			`print("lualualua")`)
		_, err := os.Stat(filepath.Join(os.TempDir(), runId+".aggregator"))
		expect.True(t, os.IsNotExist(err))
		return nil
	})
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "run_lua_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	expect.Contains(t, cmd.Args, "--chromium_build=c14d891-586101c")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func pendingChromiumBuildTask() ChromiumBuildTask {
	return ChromiumBuildTask{
		DBTask: chromium_builds.DBTask{
			CommonCols:    pendingCommonCols(),
			ChromiumRev:   "c14d891d44f0afff64e56ed7c9702df1d807b1ee",
			ChromiumRevTs: sql.NullInt64{Int64: 20080726180513, Valid: true},
			SkiaRev:       "586101c79b0490b50623e76c71a5fd67d8d92b08",
		},
	}
}

func TestChromiumBuildExecute(t *testing.T) {
	task := pendingChromiumBuildTask()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "build_chromium", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args,
		"--chromium_hash=c14d891d44f0afff64e56ed7c9702df1d807b1ee")
	expect.Contains(t, cmd.Args,
		"--skia_hash=586101c79b0490b50623e76c71a5fd67d8d92b08")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func pendingRecreatePageSetsTask() RecreatePageSetsTask {
	return RecreatePageSetsTask{
		RecreatePageSetsDBTask: admin_tasks.RecreatePageSetsDBTask{
			CommonCols: pendingCommonCols(),
			PageSets:   "All",
		},
	}
}

func TestRecreatePageSetsExecute(t *testing.T) {
	task := pendingRecreatePageSetsTask()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "create_pagesets_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func pendingRecreateWebpageArchivesTask() RecreateWebpageArchivesTask {
	return RecreateWebpageArchivesTask{
		RecreateWebpageArchivesDBTask: admin_tasks.RecreateWebpageArchivesDBTask{
			CommonCols:  pendingCommonCols(),
			PageSets:    "All",
			ChromiumRev: "c14d891d44f0afff64e56ed7c9702df1d807b1ee",
			SkiaRev:     "586101c79b0490b50623e76c71a5fd67d8d92b08",
		},
	}
}

func TestRecreateWebpageArchivesExecute(t *testing.T) {
	task := pendingRecreateWebpageArchivesTask()
	mockRun := exec.CommandCollector{}
	exec.SetRunForTesting(mockRun.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	err := task.Execute()
	assert.NoError(t, err)
	assert.Len(t, mockRun.Commands(), 1)
	cmd := mockRun.Commands()[0]
	expect.Equal(t, "capture_archives_on_workers", cmd.Name)
	expect.Contains(t, cmd.Args, "--gae_task_id=42")
	expect.Contains(t, cmd.Args, "--emails=nobody@chromium.org")
	expect.Contains(t, cmd.Args, "--pageset_type=All")
	expect.Contains(t, cmd.Args, "--chromium_build=c14d891-586101c")
	runId := getRunId(t, cmd)
	expect.Contains(t, cmd.Args, "--log_id="+runId)
	expect.NotNil(t, cmd.Timeout)
}

func TestAsPollerTask(t *testing.T) {
	expect.Nil(t, asPollerTask(nil))
	{
		taskStruct := pendingChromiumPerfTask()
		taskInterface := asPollerTask(&taskStruct.DBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*ChromiumPerfTask))
	}
	{
		taskStruct := pendingCaptureSkpsTask()
		taskInterface := asPollerTask(&taskStruct.DBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*CaptureSkpsTask))
	}
	{
		taskStruct := pendingLuaScriptTaskWithAggregator()
		taskInterface := asPollerTask(&taskStruct.DBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*LuaScriptTask))
	}
	{
		taskStruct := pendingChromiumBuildTask()
		taskInterface := asPollerTask(&taskStruct.DBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*ChromiumBuildTask))
	}
	{
		taskStruct := pendingRecreatePageSetsTask()
		taskInterface := asPollerTask(&taskStruct.RecreatePageSetsDBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*RecreatePageSetsTask))
	}
	{
		taskStruct := pendingRecreateWebpageArchivesTask()
		taskInterface := asPollerTask(&taskStruct.RecreateWebpageArchivesDBTask)
		expect.Equal(t, taskStruct, *taskInterface.(*RecreateWebpageArchivesTask))
	}
}

// Test that updateWebappTaskSetFailed works.
func TestUpdateWebappTaskSetFailed(t *testing.T) {
	task := pendingRecreateWebpageArchivesTask()
	mockServer := frontend.MockServer{}
	defer frontend.CloseTestServer(frontend.InitTestServer(&mockServer))
	err := updateWebappTaskSetFailed(&task)
	assert.NoError(t, err)
	assert.Len(t, mockServer.UpdateTaskReqs(), 1)
	updateReq := mockServer.UpdateTaskReqs()[0]
	assert.Equal(t, "/"+ctfeutil.UPDATE_RECREATE_WEBPAGE_ARCHIVES_TASK_POST_URI, updateReq.Url)
	assert.NoError(t, updateReq.Error)
	assert.False(t, updateReq.Vars.TsStarted.Valid)
	assert.True(t, updateReq.Vars.TsCompleted.Valid)
	assert.True(t, updateReq.Vars.Failure.Valid)
	assert.True(t, updateReq.Vars.Failure.Bool)
	assert.False(t, updateReq.Vars.RepeatAfterDays.Valid)
	assert.Equal(t, int64(42), updateReq.Vars.Id)
}

// Test that updateWebappTaskSetFailed returns an error when the server response indicates an error.
func TestUpdateWebappTaskSetFailedFailure(t *testing.T) {
	errstr := "You must be at least this tall to ride this ride."
	task := pendingRecreateWebpageArchivesTask()
	reqCount := 0
	mockServer := func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		assert.Equal(t, "/"+ctfeutil.UPDATE_RECREATE_WEBPAGE_ARCHIVES_TASK_POST_URI,
			r.URL.Path)
		defer skutil.Close(r.Body)
		skutil.ReportError(w, r, fmt.Errorf(errstr), "")
	}
	defer frontend.CloseTestServer(frontend.InitTestServer(http.HandlerFunc(mockServer)))
	err := updateWebappTaskSetFailed(&task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), errstr)
	assert.Equal(t, 1, reqCount)
}

func TestDoWorkerHealthCheck(t *testing.T) {
	mockExec := exec.CommandCollector{}
	exec.SetRunForTesting(mockExec.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	doWorkerHealthCheck()
	// Expect three commands: git pull; make all; check_workers_health --log_dir=/b/storage/glog
	commands := mockExec.Commands()
	assert.Len(t, commands, 3)
	expect.Equal(t, "git pull", exec.DebugString(commands[0]))
	expect.Equal(t, "make all", exec.DebugString(commands[1]))
	expect.Equal(t, "check_workers_health --log_dir=/b/storage/glog",
		exec.DebugString(commands[2]))
}

// Test that an error executing check_workers_health does not bubble up.
func TestDoWorkerHealthCheckError(t *testing.T) {
	commandCollector := exec.CommandCollector{}
	mockRun := exec.MockRun{}
	commandCollector.SetDelegateRun(mockRun.Run)
	exec.SetRunForTesting(commandCollector.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	mockRun.AddRule("check_workers_health", fmt.Errorf("I'm not a doctor."))
	// Expect error to be logged and ignored.
	doWorkerHealthCheck()
	// Expect three commands: git pull; make all; check_workers_health --log_dir=/b/storage/glog
	commands := commandCollector.Commands()
	assert.Len(t, commands, 3)
	expect.Equal(t, "git pull", exec.DebugString(commands[0]))
	expect.Equal(t, "make all", exec.DebugString(commands[1]))
	expect.Equal(t, "check_workers_health --log_dir=/b/storage/glog",
		exec.DebugString(commands[2]))
}

func TestPollAndExecOnce(t *testing.T) {
	task := pendingRecreateWebpageArchivesTask()
	mockServer := frontend.MockServer{}
	mockServer.SetCurrentTask(&task.RecreateWebpageArchivesDBTask)
	defer frontend.CloseTestServer(frontend.InitTestServer(&mockServer))
	mockExec := exec.CommandCollector{}
	exec.SetRunForTesting(mockExec.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	pollAndExecOnce()
	// Expect only one poll.
	expect.Equal(t, 1, mockServer.OldestPendingTaskReqCount())
	// Expect three commands: git pull; make all; capture_archives_on_workers ...
	commands := mockExec.Commands()
	assert.Len(t, commands, 3)
	expect.Equal(t, "git pull", exec.DebugString(commands[0]))
	expect.Equal(t, "make all", exec.DebugString(commands[1]))
	expect.Equal(t, "capture_archives_on_workers", commands[2].Name)
	// No updates expected. (capture_archives_on_workers would send updates if it were
	// executed.)
	expect.Empty(t, mockServer.UpdateTaskReqs())
}

func TestPollAndExecOnceMultipleTasks(t *testing.T) {
	task1 := pendingRecreateWebpageArchivesTask()
	mockServer := frontend.MockServer{}
	mockServer.SetCurrentTask(&task1.RecreateWebpageArchivesDBTask)
	defer frontend.CloseTestServer(frontend.InitTestServer(&mockServer))
	mockExec := exec.CommandCollector{}
	exec.SetRunForTesting(mockExec.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	// Poll frontend and execute the first task.
	pollAndExecOnce()
	// Update current task.
	task2 := pendingChromiumPerfTask()
	mockServer.SetCurrentTask(&task2.DBTask)
	// Poll frontend and execute the second task.
	pollAndExecOnce()

	// Expect two pending task requests.
	expect.Equal(t, 2, mockServer.OldestPendingTaskReqCount())
	// Expect six commands: git pull; make all; capture_archives_on_workers ...; git pull;
	// make all; run_chromium_perf_on_workers ...
	commands := mockExec.Commands()
	assert.Len(t, commands, 6)
	expect.Equal(t, "git pull", exec.DebugString(commands[0]))
	expect.Equal(t, "make all", exec.DebugString(commands[1]))
	expect.Equal(t, "capture_archives_on_workers", commands[2].Name)
	expect.Equal(t, "git pull", exec.DebugString(commands[3]))
	expect.Equal(t, "make all", exec.DebugString(commands[4]))
	expect.Equal(t, "run_chromium_perf_on_workers", commands[5].Name)
	// No updates expected when commands succeed.
	expect.Empty(t, mockServer.UpdateTaskReqs())
}

func TestPollAndExecOnceError(t *testing.T) {
	task := pendingRecreateWebpageArchivesTask()
	mockServer := frontend.MockServer{}
	mockServer.SetCurrentTask(&task.RecreateWebpageArchivesDBTask)
	defer frontend.CloseTestServer(frontend.InitTestServer(&mockServer))
	commandCollector := exec.CommandCollector{}
	mockRun := exec.MockRun{}
	commandCollector.SetDelegateRun(mockRun.Run)
	exec.SetRunForTesting(commandCollector.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	mockRun.AddRule("capture_archives_on_workers", fmt.Errorf("workers too lazy"))
	pollAndExecOnce()
	// Expect only one poll.
	expect.Equal(t, 1, mockServer.OldestPendingTaskReqCount())
	// Expect three commands: git pull; make all; capture_archives_on_workers ...
	commands := commandCollector.Commands()
	assert.Len(t, commands, 3)
	expect.Equal(t, "git pull", exec.DebugString(commands[0]))
	expect.Equal(t, "make all", exec.DebugString(commands[1]))
	expect.Equal(t, "capture_archives_on_workers", commands[2].Name)
	// Expect an update marking task failed when command fails to execute.
	assert.Len(t, mockServer.UpdateTaskReqs(), 1)
	updateReq := mockServer.UpdateTaskReqs()[0]
	assert.Equal(t, "/"+ctfeutil.UPDATE_RECREATE_WEBPAGE_ARCHIVES_TASK_POST_URI, updateReq.Url)
	assert.NoError(t, updateReq.Error)
	assert.False(t, updateReq.Vars.TsStarted.Valid)
	assert.True(t, updateReq.Vars.TsCompleted.Valid)
	assert.True(t, updateReq.Vars.Failure.Valid)
	assert.True(t, updateReq.Vars.Failure.Bool)
	assert.False(t, updateReq.Vars.RepeatAfterDays.Valid)
	assert.Equal(t, int64(42), updateReq.Vars.Id)
}

func TestPollAndExecOnceNoTasks(t *testing.T) {
	mockServer := frontend.MockServer{}
	mockServer.SetCurrentTask(nil)
	defer frontend.CloseTestServer(frontend.InitTestServer(&mockServer))
	mockExec := exec.CommandCollector{}
	exec.SetRunForTesting(mockExec.Run)
	defer exec.SetRunForTesting(exec.DefaultRun)
	// Poll frontend, no tasks.
	pollAndExecOnce()
	pollAndExecOnce()
	pollAndExecOnce()
	// Expect three polls.
	expect.Equal(t, 3, mockServer.OldestPendingTaskReqCount())
	// Expect no commands.
	expect.Empty(t, mockExec.Commands())
	// No updates expected.
	expect.Empty(t, mockServer.UpdateTaskReqs())
}