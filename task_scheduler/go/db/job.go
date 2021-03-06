package db

import (
	"bytes"
	"encoding/gob"
	"sync"
	"time"
)

const (
	// JOB_STATUS_IN_PROGRESS indicates that one or more of the Job's
	// Task dependencies has not yet been satisfied.
	JOB_STATUS_IN_PROGRESS JobStatus = ""

	// JOB_STATUS_SUCCESS indicates that all of the Job's Task dependencies
	// completed successfully.
	JOB_STATUS_SUCCESS JobStatus = "SUCCESS"

	// JOB_STATUS_FAILURE indicates that one or more of the Job's Task
	// dependencies failed.
	JOB_STATUS_FAILURE JobStatus = "FAILURE"

	// JOB_STATUS_MISHAP indicates that one or more of the Job's Task
	// dependencies exited early with an error, died while in progress, was
	// manually canceled, expired while waiting on the queue, or timed out
	// before completing.
	JOB_STATUS_MISHAP JobStatus = "MISHAP"

	// JOB_STATUS_CANCELED indicates that the Job has been canceled.
	JOB_STATUS_CANCELED JobStatus = "CANCELED"
)

var (
	JOB_STATUS_BADNESS = map[JobStatus]int{
		JOB_STATUS_SUCCESS:     0,
		JOB_STATUS_IN_PROGRESS: 1,
		JOB_STATUS_CANCELED:    2,
		JOB_STATUS_FAILURE:     3,
		JOB_STATUS_MISHAP:      4,
	}
)

// JobStatus represents the current status of a Job. A JobStatus other than
// JOB_STATUS_IN_PROGRESS is final; we do not retry Jobs, only their component
// Tasks.
type JobStatus string

// WorseThan returns true iff this JobStatus is worse than the given JobStatus.
func (s JobStatus) WorseThan(other JobStatus) bool {
	return JOB_STATUS_BADNESS[s] > JOB_STATUS_BADNESS[other]
}

// WorseJobStatus returns the worse of the two JobStatus.
func WorseJobStatus(a, b JobStatus) JobStatus {
	if a.WorseThan(b) {
		return a
	}
	return b
}

// JobStatusFromTaskStatus returns a JobStatus based on a TaskStatus.
func JobStatusFromTaskStatus(s TaskStatus) JobStatus {
	switch s {
	case TASK_STATUS_SUCCESS:
		return JOB_STATUS_SUCCESS
	case TASK_STATUS_FAILURE:
		return JOB_STATUS_FAILURE
	case TASK_STATUS_MISHAP:
		return JOB_STATUS_MISHAP
	}
	return JOB_STATUS_IN_PROGRESS
}

// Job represents a set of Tasks which are executed as part of a larger effort.
//
// Job is stored as a GOB, so changes must maintain backwards compatibility.
// See gob package documentation for details, but generally:
//   - Ensure new fields can be initialized with their zero value.
//   - Do not change the type of any existing field.
//   - Leave removed fields commented out to ensure the field name is not
//     reused.
//   - Add any new fields to the Copy() method.
type Job struct {
	// Created is the creation timestamp. This property should never change
	// for a given Job instance.
	Created time.Time

	// DbModified is the time of the last successful call to JobDB.PutJob/s
	// for this Job, or zero if the job is new.
	DbModified time.Time

	// Dependencies are the names of the TaskSpecs on which this Job
	// depends.  This property should never change for a given Job instance.
	Dependencies []string

	// Finished is the time at which all of the Job's dependencies finished,
	// successfully or not.
	Finished time.Time

	// Id is a unique identifier for the Job. This property should never
	// change for a given Job instance, after its initial insertion into the
	// DB.
	Id string

	// Name is a human-friendly descriptive name for the Job. All Jobs
	// generated from the same JobSpec have the same name. This property
	// should never change for a given Job instance.
	Name string

	// Priority is an indicator of the relative priority of this Job.
	Priority float64

	// Repo is the repository of the commit at which this Job ran. This
	// property should never change for a given Job instance.
	Repo string

	// Revision is the commit at which this Job ran. This property should
	// never change for a given Job instance.
	Revision string

	// Status is the current Job status, default JOB_STATUS_IN_PROGRESS.
	Status JobStatus
}

// Copy returns a copy of the Job.
func (j *Job) Copy() *Job {
	var deps []string
	if j.Dependencies != nil {
		deps = make([]string, len(j.Dependencies))
		copy(deps, j.Dependencies)
	}
	return &Job{
		Created:      j.Created,
		DbModified:   j.DbModified,
		Dependencies: deps,
		Finished:     j.Finished,
		Id:           j.Id,
		Name:         j.Name,
		Priority:     j.Priority,
		Repo:         j.Repo,
		Revision:     j.Revision,
		Status:       j.Status,
	}
}

func (j *Job) Done() bool {
	return j.Status != JOB_STATUS_IN_PROGRESS
}

// JobSlice implements sort.Interface. To sort jobs []*Job, use
// sort.Sort(JobSlice(jobs)).
type JobSlice []*Job

func (s JobSlice) Len() int { return len(s) }

func (s JobSlice) Less(i, j int) bool {
	return s[i].Created.Before(s[j].Created)
}

func (s JobSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// JobEncoder encodes Jobs into bytes via GOB encoding. Not safe for
// concurrent use.
// TODO(benjaminwagner): Encode in parallel.
type JobEncoder struct {
	err    error
	jobs   []*Job
	result [][]byte
}

// Process encodes the Job into a byte slice that will be returned from Next()
// (in arbitrary order). Returns false if Next is certain to return an error.
// Caller must ensure j does not change until after the first call to Next().
// May not be called after calling Next().
func (e *JobEncoder) Process(j *Job) bool {
	if e.err != nil {
		return false
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(j); err != nil {
		e.err = err
		e.jobs = nil
		e.result = nil
		return false
	}
	e.jobs = append(e.jobs, j)
	e.result = append(e.result, buf.Bytes())
	return true
}

// Next returns one of the Jobs provided to Process (in arbitrary order) and
// its serialized bytes. If any jobs remain, returns the job, the serialized
// bytes, nil. If all jobs have been returned, returns nil, nil, nil. If an
// error is encountered, returns nil, nil, error.
func (e *JobEncoder) Next() (*Job, []byte, error) {
	if e.err != nil {
		return nil, nil, e.err
	}
	if len(e.jobs) == 0 {
		return nil, nil, nil
	}
	j := e.jobs[0]
	e.jobs = e.jobs[1:]
	serialized := e.result[0]
	e.result = e.result[1:]
	return j, serialized, nil
}

// JobDecoder decodes bytes into Jobs via GOB decoding. Not safe for
// concurrent use.
type JobDecoder struct {
	// input contains the incoming byte slices. Process() sends on this channel,
	// decode() receives from it, and Result() closes it.
	input chan []byte
	// output contains decoded Jobs. decode() sends on this channel, collect()
	// receives from it, and run() closes it when all decode() goroutines have
	// finished.
	output chan *Job
	// result contains the return value of Result(). collect() sends a single
	// value on this channel and closes it. Result() receives from it.
	result chan []*Job
	// errors contains the first error from any goroutine. It's a channel in case
	// multiple goroutines experience an error at the same time.
	errors chan error
}

// init initializes d if it has not been initialized. May not be called concurrently.
func (d *JobDecoder) init() {
	if d.input == nil {
		d.input = make(chan []byte, kNumDecoderGoroutines*2)
		d.output = make(chan *Job, kNumDecoderGoroutines)
		d.result = make(chan []*Job, 1)
		d.errors = make(chan error, kNumDecoderGoroutines)
		go d.run()
		go d.collect()
	}
}

// run starts the decode goroutines and closes d.output when they finish.
func (d *JobDecoder) run() {
	// Start decoders.
	wg := sync.WaitGroup{}
	for i := 0; i < kNumDecoderGoroutines; i++ {
		wg.Add(1)
		go d.decode(&wg)
	}
	// Wait for decoders to exit.
	wg.Wait()
	// Drain d.input in the case that errors were encountered, to avoid deadlock.
	for _ = range d.input {
	}
	close(d.output)
}

// decode receives from d.input and sends to d.output until d.input is closed or
// d.errors is non-empty. Decrements wg when done.
func (d *JobDecoder) decode(wg *sync.WaitGroup) {
	for b := range d.input {
		var j Job
		if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&j); err != nil {
			d.errors <- err
			break
		}
		d.output <- &j
		if len(d.errors) > 0 {
			break
		}
	}
	wg.Done()
}

// collect receives from d.output until it is closed, then sends on d.result.
func (d *JobDecoder) collect() {
	result := []*Job{}
	for j := range d.output {
		result = append(result, j)
	}
	d.result <- result
	close(d.result)
}

// Process decodes the byte slice into a Job and includes it in Result() (in
// arbitrary order). Returns false if Result is certain to return an error.
// Caller must ensure b does not change until after Result() returns.
func (d *JobDecoder) Process(b []byte) bool {
	d.init()
	d.input <- b
	return len(d.errors) == 0
}

// Result returns all decoded Jobs provided to Process (in arbitrary order), or
// any error encountered.
func (d *JobDecoder) Result() ([]*Job, error) {
	// Allow JobDecoder to be used without initialization.
	if d.result == nil {
		return []*Job{}, nil
	}
	close(d.input)
	select {
	case err := <-d.errors:
		return nil, err
	case result := <-d.result:
		return result, nil
	}
}
