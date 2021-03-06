package autoroller

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"
	"go.skia.org/infra/autoroll/go/autoroll_modes"
	"go.skia.org/infra/autoroll/go/repo_manager"
	"go.skia.org/infra/go/autoroll"
	"go.skia.org/infra/go/buildbucket"
	"go.skia.org/infra/go/mockhttpclient"
	"go.skia.org/infra/go/rietveld"
	"go.skia.org/infra/go/testutils"
)

const COMMITTED_STR = "Committed: https://chromium.googlesource.com/chromium/src/+/fd01dc2938"

var noTrybots = []*buildbucket.Build{}

// mockRepoManager is a struct used for mocking out the AutoRoller's
// interactions with a RepoManager.
type mockRepoManager struct {
	forceUpdateCount    int
	mockIssueNumber     int64
	mockFullChildHashes map[string]string
	lastRollRev         string
	rolledPast          map[string]bool
	skiaHead            string
	mtx                 sync.RWMutex
	t                   *testing.T
}

// ForceUpdate pretends to force the mockRepoManager to update.
func (r *mockRepoManager) ForceUpdate() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.forceUpdateCount == 0 {
		return fmt.Errorf("forceUpdateCount == 0!")
	}
	r.forceUpdateCount--
	return nil
}

// mockForceUpdate increments the expected ForceUpdate call count.
func (r *mockRepoManager) mockForceUpdate() {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.forceUpdateCount++
}

// assertForceUpdate asserts that the ForceUpdate call count is zero.
func (r *mockRepoManager) assertForceUpdate() {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	assert.Equal(r.t, 0, r.forceUpdateCount)
}

// getForceUpdateCount returns the remaining ForceUpdate call count.
func (r *mockRepoManager) getForceUpdateCount() int {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.forceUpdateCount
}

// FullChildHash returns the full hash of the given short hash or ref in the
// mocked child repo.
func (r *mockRepoManager) FullChildHash(shortHash string) (string, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	h, ok := r.mockFullChildHashes[shortHash]
	if !ok {
		return "", fmt.Errorf("Unknown short hash: %s", shortHash)
	}
	return h, nil
}

// mockFullChildHash adds the given mock hash.
func (r *mockRepoManager) mockFullChildHash(short, long string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.mockFullChildHashes[short] = long
}

// LastRollRev returns the last-rolled child commit in the mocked repo.
func (r *mockRepoManager) LastRollRev() string {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.lastRollRev
}

// mockLastRollRev fakes the last roll revision.
func (r *mockRepoManager) mockLastRollRev(last string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.lastRollRev = last
}

// RolledPast determines whether DEPS has rolled past the given commit in the
// mocked repo.
func (r *mockRepoManager) RolledPast(hash string) bool {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	rv, ok := r.rolledPast[hash]
	if !ok {
		r.t.Fatal(fmt.Sprintf("Unknown hash: %s", hash))
	}
	return rv
}

// mockRolledPast pretends that the DEPS has rolled past the given commit.
func (r *mockRepoManager) mockRolledPast(hash string, rolled bool) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.rolledPast[hash] = rolled
}

// ChildHead returns the current child origin/master branch head in the mocked
// repo.
func (r *mockRepoManager) ChildHead() string {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.skiaHead
}

// mockChildHead sets the fake child origin/master branch head.
func (r *mockRepoManager) mockChildHead(hash string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.skiaHead = hash
}

// CreateNewRoll pretends to create a new DEPS roll from the mocked repo,
// returning the fake issue number set by the test.
func (r *mockRepoManager) CreateNewRoll(emails []string, cqExtraTrybots string, dryRun bool) (int64, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.mockIssueNumber, nil
}

// mockChildCommit pretends that a child commit has landed.
func (r *mockRepoManager) mockChildCommit(hash string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.mockFullChildHashes == nil {
		r.mockFullChildHashes = map[string]string{}
	}
	if r.rolledPast == nil {
		r.rolledPast = map[string]bool{}
	}
	assert.Equal(r.t, 40, len(hash))
	shortHash := hash[:12]
	r.skiaHead = hash
	r.mockFullChildHashes[shortHash] = hash
	r.rolledPast[hash] = false
}

// rollerWillUpload sets up expectations for the roller to upload a CL. Returns
// a rietveld.Issue representing the new, in-progress DEPS roll.
func (r *mockRepoManager) rollerWillUpload(rv *mockRietveld, from, to string, tryResults []*buildbucket.Build, dryRun bool) *rietveld.Issue {
	emails := []string{"test-sheriff@google.com"}
	// Rietveld API only has millisecond precision.
	now := time.Now().UTC().Round(time.Millisecond)
	description := fmt.Sprintf(`Roll src/third_party/skia/ %s..%s (42 commits).

blah blah
TBR=some-sheriff
`, from[:12], to[:12])
	subject := strings.Split(description, "\n")[0]
	r.mockIssueNumber = rv.nextIssueNum()
	roll := &rietveld.Issue{
		CC:                emails,
		CommitQueue:       true,
		CommitQueueDryRun: dryRun,
		Created:           now,
		CreatedString:     now.Format(rietveld.TIME_FORMAT),
		Description:       description,
		Issue:             r.mockIssueNumber,
		Messages:          []rietveld.IssueMessage{},
		Modified:          now,
		ModifiedString:    now.Format(rietveld.TIME_FORMAT),
		Owner:             autoroll.ROLL_AUTHOR,
		Project:           "skia",
		Reviewers:         emails,
		Subject:           subject,
		Patchsets:         []int64{1},
	}
	rv.updateIssue(roll, tryResults)
	return roll
}

func (r *mockRepoManager) User() string {
	return "test_user"
}

// mockRietveld is a struct used for faking responses from Rietveld.
type mockRietveld struct {
	fakeIssueNum int64
	r            *rietveld.Rietveld
	t            *testing.T
	urlMock      *mockhttpclient.URLMock
}

// assertMocksEmpty asserts that all of the URLs in the URLMock have been used.
func (r *mockRietveld) assertMocksEmpty() {
	assert.True(r.t, r.urlMock.Empty())
}

// mockTrybotResults sets up a fake response to a request for trybot results.
func (r *mockRietveld) mockTrybotResults(issue *rietveld.Issue, results []*buildbucket.Build) {
	url := fmt.Sprintf("https://cr-buildbucket.appspot.com/_ah/api/buildbucket/v1/search?tag=buildset%%3Apatch%%2Frietveld%%2Fcodereview.chromium.org%%2F%d%%2F1", issue.Issue)
	serialized, err := json.Marshal(struct {
		Builds []*buildbucket.Build
	}{
		Builds: results,
	})
	assert.NoError(r.t, err)
	r.urlMock.MockOnce(url, mockhttpclient.MockGetDialogue(serialized))
}

// updateIssue inserts or updates the issue in the mockRietveld.
func (r *mockRietveld) updateIssue(issue *rietveld.Issue, tryResults []*buildbucket.Build) {
	url := fmt.Sprintf("%s/api/%d?messages=true", autoroll.RIETVELD_URL, issue.Issue)
	serialized, err := json.Marshal(issue)
	assert.NoError(r.t, err)
	r.urlMock.MockOnce(url, mockhttpclient.MockGetDialogue(serialized))
	r.mockTrybotResults(issue, tryResults)

	// If necessary, fake the CQ status URL. This is used whenever the rietveld package
	// cannot find the "Committed: ..." string within the CL description.
	if !strings.Contains(issue.Description, COMMITTED_STR) {
		cqUrl := fmt.Sprintf(rietveld.CQ_STATUS_URL, issue.Issue, issue.Patchsets[len(issue.Patchsets)-1])
		r.urlMock.MockOnce(cqUrl, mockhttpclient.MockGetDialogue([]byte(fmt.Sprintf("{\"success\":%v}", false))))
	}
}

// modify changes the last-modified timestamp of the roll and updates it in the
// mockRietveld.
func (r *mockRietveld) modify(issue *rietveld.Issue, tryResults []*buildbucket.Build) {
	now := time.Now().UTC().Round(time.Millisecond)
	issue.Modified = now
	issue.ModifiedString = now.Format(rietveld.TIME_FORMAT)
	r.updateIssue(issue, tryResults)
}

// rollerWillCloseIssue sets expectations for the roller to close the issue.
func (r *mockRietveld) rollerWillCloseIssue(issue *rietveld.Issue) {
	p := mockhttpclient.MockPostDialogue("application/x-www-form-urlencoded", mockhttpclient.DONT_CARE_REQUEST, []byte{})
	r.urlMock.MockOnce(fmt.Sprintf("%s/%d/publish", autoroll.RIETVELD_URL, issue.Issue), p)
	r.urlMock.MockOnce(fmt.Sprintf("%s/%d/close", autoroll.RIETVELD_URL, issue.Issue), p)
}

// rollerWillSwitchDryRun sets expectations for the roller to switch the issue
// into or out of dry run mode.
func (r *mockRietveld) rollerWillSwitchDryRun(issue *rietveld.Issue, tryResults []*buildbucket.Build, dryRun bool) {
	r.updateIssue(issue, tryResults) // Initial issue update.
	p := mockhttpclient.MockPostDialogue("application/x-www-form-urlencoded", mockhttpclient.DONT_CARE_REQUEST, []byte{})
	r.urlMock.MockOnce(fmt.Sprintf("%s/%d/edit_flags", autoroll.RIETVELD_URL, issue.Issue), p)
	r.urlMock.MockOnce(fmt.Sprintf("%s/%d/edit_flags", autoroll.RIETVELD_URL, issue.Issue), p)
	issue.CommitQueueDryRun = dryRun
	r.updateIssue(issue, tryResults) // Update the issue after setting flags.
}

// pretendDryRunFinished sets expectations for when the dry run has finished.
func (r *mockRietveld) pretendDryRunFinished(issue *rietveld.Issue, tryResults []*buildbucket.Build, success bool) {
	result := autoroll.TRYBOT_RESULT_FAILURE
	if success {
		result = autoroll.TRYBOT_RESULT_SUCCESS
	}
	for _, t := range tryResults {
		t.Status = autoroll.TRYBOT_STATUS_COMPLETED
		t.Result = result
	}
	issue.CommitQueue = false
	issue.CommitQueueDryRun = false
	r.updateIssue(issue, tryResults) // Initial issue update.

	// The roller will add a comment to the issue and close it if the dry run failed.
	if success {
		p := mockhttpclient.MockPostDialogue("application/x-www-form-urlencoded", mockhttpclient.DONT_CARE_REQUEST, []byte{})
		r.urlMock.MockOnce(fmt.Sprintf("%s/%d/publish", autoroll.RIETVELD_URL, issue.Issue), p)
		r.updateIssue(issue, tryResults) // Update the issue after adding a comment.
	} else {
		r.rollerWillCloseIssue(issue)
	}
}

// pretendRollFailed changes the roll to appear to have failed in the
// mockRietveld.
func (r *mockRietveld) pretendRollFailed(issue *rietveld.Issue, tryResults []*buildbucket.Build) {
	issue.CommitQueue = false
	issue.CommitQueueDryRun = false
	r.modify(issue, tryResults)
}

// pretendRollLanded changes the roll to appear to have succeeded in the
// mockRietveld.
func (r *mockRietveld) pretendRollLanded(rm *mockRepoManager, issue *rietveld.Issue, tryResults []*buildbucket.Build) {
	// Determine what revision we rolled to.
	m := autoroll.ROLL_REV_REGEX.FindStringSubmatch(issue.Subject)
	assert.NotNil(r.t, m)
	assert.Equal(r.t, 3, len(m))
	rolledTo, err := rm.FullChildHash(m[2])
	assert.NoError(r.t, err)
	rm.mockRolledPast(rolledTo, true)
	rm.mockLastRollRev(rolledTo)
	rm.mockForceUpdate()

	issue.Closed = true
	issue.Committed = true
	issue.CommitQueue = false
	issue.CommitQueueDryRun = false
	issue.Description += "\n" + COMMITTED_STR
	r.modify(issue, tryResults)
}

// nextIssueNum provides auto-incrementing fake issue numbers.
func (r *mockRietveld) nextIssueNum() int64 {
	n := r.fakeIssueNum
	r.fakeIssueNum++
	return n
}

// checkStatus verifies that we get the expected status from the roller.
func checkStatus(t *testing.T, r *AutoRoller, rv *mockRietveld, rm *mockRepoManager, expectedStatus string, current *rietveld.Issue, currentTrybots []*buildbucket.Build, currentDryRun bool, last *rietveld.Issue, lastTrybots []*buildbucket.Build, lastDryRun bool) {
	rv.assertMocksEmpty()
	rm.assertForceUpdate()
	s := r.GetStatus(true)
	assert.Equal(t, expectedStatus, s.Status)
	assert.Equal(t, s.Error, "")
	checkRoll := func(t *testing.T, expect *rietveld.Issue, actual *autoroll.AutoRollIssue, expectTrybots []*buildbucket.Build, dryRun bool) {
		if expect != nil {
			assert.NotNil(t, actual)
			ari, err := autoroll.FromRietveldIssue(expect, rm.FullChildHash)
			assert.NoError(t, err)
			tryResults := make([]*autoroll.TryResult, 0, len(expectTrybots))
			for _, b := range expectTrybots {
				tryResult, err := autoroll.TryResultFromBuildbucket(b)
				assert.NoError(t, err)
				tryResults = append(tryResults, tryResult)
			}
			ari.TryResults = tryResults

			// This is kind of a hack to prevent having to pass the
			// expected dry run result around.
			if dryRun {
				if ari.AllTrybotsFinished() {
					ari.Result = autoroll.ROLL_RESULT_DRY_RUN_FAILURE
					if ari.AllTrybotsSucceeded() {
						ari.Result = autoroll.ROLL_RESULT_DRY_RUN_SUCCESS
					}
				}
			}

			assert.NoError(t, ari.Validate())
			testutils.AssertDeepEqual(t, ari, actual)
		} else {
			assert.Nil(t, actual)
		}
	}
	checkRoll(t, current, s.CurrentRoll, currentTrybots, currentDryRun)
	checkRoll(t, last, s.LastRoll, lastTrybots, lastDryRun)
}

// setup initializes a fake AutoRoller for testing. It returns the working
// directory, AutoRoller instance, URLMock for faking HTTP requests, and an
// rietveld.Issue representing the first CL that was uploaded by the AutoRoller.
func setup(t *testing.T) (string, *AutoRoller, *mockRepoManager, *mockRietveld, *rietveld.Issue) {
	testutils.SkipIfShort(t)

	// Setup mocks.
	urlMock := mockhttpclient.NewURLMock()
	urlMock.Mock(fmt.Sprintf("%s/xsrf_token", autoroll.RIETVELD_URL), mockhttpclient.MockGetDialogue([]byte("abc123")))
	rv := &mockRietveld{
		fakeIssueNum: 10001,
		r:            rietveld.New(autoroll.RIETVELD_URL, urlMock.Client()),
		t:            t,
		urlMock:      urlMock,
	}

	rm := &mockRepoManager{t: t}
	repo_manager.NewRepoManager = func(workdir, childPath string, frequency time.Duration, depot_tools string) (repo_manager.RepoManager, error) {
		return rm, nil
	}

	workdir, err := ioutil.TempDir("", "test_autoroll_mode_")
	assert.NoError(t, err)

	// Set up more test data.
	initialCommit := "abc1231010101010101010101010101010101010"
	rm.mockChildCommit(initialCommit)
	rm.mockChildCommit("def4561010101010101010101010101010101010")
	rm.mockLastRollRev(initialCommit)
	rm.mockRolledPast(initialCommit, true)
	roll1 := rm.rollerWillUpload(rv, rm.LastRollRev(), rm.ChildHead(), noTrybots, false)

	// Create the roller.
	roller, err := NewAutoRoller(workdir, "src/third_party/skia", "", []string{}, rv.r, time.Hour, time.Hour, "depot_tools")
	assert.NoError(t, err)

	// Verify that the bot ran successfully.
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll1, noTrybots, false, nil, nil, false)

	return workdir, roller, rm, rv, roll1
}

// TestAutoRollBasic ensures that the typical function of the AutoRoller works
// as expected.
func TestAutoRollBasic(t *testing.T) {
	// setup will initialize the roller and upload a CL.
	workdir, roller, rm, rv, roll1 := setup(t)
	defer func() {
		assert.NoError(t, roller.Close())
		assert.NoError(t, os.RemoveAll(workdir))
	}()

	// Run again. Verify that we let the currently-running roll keep going.
	rv.updateIssue(roll1, noTrybots)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll1, noTrybots, false, nil, nil, false)

	// The roll failed. Verify that we close it and upload another one.
	rv.pretendRollFailed(roll1, noTrybots)
	rv.rollerWillCloseIssue(roll1)
	roll2 := rm.rollerWillUpload(rv, rm.LastRollRev(), rm.ChildHead(), noTrybots, false)
	assert.NoError(t, roller.doAutoRoll())
	roll1.Closed = true // The roller should have closed this CL.
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll2, noTrybots, false, roll1, noTrybots, false)

	// The second roll succeeded. Verify that we're up-to-date.
	rv.pretendRollLanded(rm, roll2, noTrybots)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_UP_TO_DATE, nil, nil, false, roll2, noTrybots, false)

	// Verify that we remain idle.
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_UP_TO_DATE, nil, nil, false, roll2, noTrybots, false)
}

// TestAutoRollStop ensures that we can properly stop and restart the
// AutoRoller.
func TestAutoRollStop(t *testing.T) {
	// setup will initialize the roller and upload a CL.
	workdir, roller, rm, rv, roll1 := setup(t)
	defer func() {
		assert.NoError(t, roller.Close())
		assert.NoError(t, os.RemoveAll(workdir))
	}()

	// Stop the bot. Ensure that we close the in-progress roll and don't upload a new one.
	rv.updateIssue(roll1, noTrybots)
	rv.rollerWillCloseIssue(roll1)
	// After the roller closes the CL, it will grab its info from Rietveld
	// and expect the CQ bit to be unset. and the issue to be closed.
	roll1.CommitQueue = false
	roll1.Closed = true
	// Change the mode, run the bot.
	u := "test@google.com"
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_STOPPED, u, "Stoppit!"))
	// The roller should have closed the CL.
	roll1.Closed = true
	roll1.CommitQueue = false
	roll1.CommitQueueDryRun = false
	checkStatus(t, roller, rv, rm, STATUS_STOPPED, nil, nil, false, roll1, noTrybots, false)

	// Ensure that we don't upload another CL now that we're stopped.
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_STOPPED, nil, nil, false, roll1, noTrybots, false)

	// Resume the bot. Ensure that we upload a new CL.
	roll2 := rm.rollerWillUpload(rv, rm.LastRollRev(), rm.ChildHead(), noTrybots, false)
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_RUNNING, u, "Resume!"))
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll2, noTrybots, false, roll1, noTrybots, false)

	// Pretend the roll landed.
	rv.pretendRollLanded(rm, roll2, noTrybots)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_UP_TO_DATE, nil, nil, false, roll2, noTrybots, false)

	// Stop the roller again.
	rm.mockChildCommit("adbcdf1010101010101010101010101010101010")
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_STOPPED, u, "Stop!"))
	checkStatus(t, roller, rv, rm, STATUS_STOPPED, nil, nil, false, roll2, noTrybots, false)

	// Ensure that we don't upload another CL now that we're stopped.
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_STOPPED, nil, nil, false, roll2, noTrybots, false)
}

// TestAutoRollDryRun ensures that the Dry Run functionalify works as expected.
func TestAutoRollDryRun(t *testing.T) {
	workdir, roller, rm, rv, roll1 := setup(t)
	defer func() {
		assert.NoError(t, roller.Close())
		assert.NoError(t, os.RemoveAll(workdir))
	}()

	// Change the mode to dry run. Expect the bot to switch the in-progress
	// roll to a dry run. There is one unfinished trybot.
	u := "test@google.com"
	trybot := &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Status:           autoroll.TRYBOT_STATUS_STARTED,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq\"}",
	}
	trybots := []*buildbucket.Build{trybot}
	rv.rollerWillSwitchDryRun(roll1, trybots, true)
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_DRY_RUN, u, "Dry run."))
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_IN_PROGRESS, roll1, trybots, true, nil, nil, false)

	// Dry run succeeded.
	rv.pretendDryRunFinished(roll1, trybots, true)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_SUCCESS, roll1, trybots, true, nil, nil, false)

	// Run again. Ensure that we don't do anything crazy.
	rv.updateIssue(roll1, trybots)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_SUCCESS, roll1, trybots, true, nil, nil, false)

	// Add an experimental trybot. Ensure that its failure is ignored.
	trybots = append(trybots, &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Result:           autoroll.TRYBOT_RESULT_FAILURE,
		Status:           autoroll.TRYBOT_STATUS_COMPLETED,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq-experimental\"}",
	})
	rv.updateIssue(roll1, trybots)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_SUCCESS, roll1, trybots, true, nil, nil, false)

	// New child commit: verify that we close the existing dry run and open another.
	rm.mockChildCommit("adbcdf1010101010101010101010101010101010")
	rv.updateIssue(roll1, trybots)
	rv.rollerWillCloseIssue(roll1)
	trybot2 := &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Status:           autoroll.TRYBOT_STATUS_STARTED,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq\"}",
	}
	trybots2 := []*buildbucket.Build{trybot2}
	roll2 := rm.rollerWillUpload(rv, rm.LastRollRev(), rm.ChildHead(), trybots2, true)
	roll2.CommitQueueDryRun = true
	assert.NoError(t, roller.doAutoRoll())
	roll1.Closed = true // Roller should have closed this issue.
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_IN_PROGRESS, roll2, trybots2, true, roll1, trybots, true)

	// Dry run failed. Ensure that we close the roll and open another, same
	// as in non-dry-run mode.
	rv.pretendDryRunFinished(roll2, trybots2, false)
	trybot3 := &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Status:           autoroll.TRYBOT_STATUS_STARTED,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq\"}",
	}
	trybots3 := []*buildbucket.Build{trybot3}
	roll3 := rm.rollerWillUpload(rv, rm.LastRollRev(), rm.ChildHead(), trybots3, true)
	assert.NoError(t, roller.doAutoRoll())
	roll2.Closed = true // Roller should have closed this issue.
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_IN_PROGRESS, roll3, trybots3, true, roll2, trybots2, true)

	// Ensure that we switch back to normal running mode as expected.
	rv.rollerWillSwitchDryRun(roll3, trybots3, false)
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_RUNNING, u, "Normal mode."))
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll3, trybots3, false, roll2, trybots2, true)

	// Switch back to dry run.
	rv.rollerWillSwitchDryRun(roll3, trybots3, true)
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_DRY_RUN, u, "Dry run again."))
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_IN_PROGRESS, roll3, trybots3, true, roll2, trybots2, true)

	// Dry run succeeded.
	rv.pretendDryRunFinished(roll3, trybots3, true)
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_DRY_RUN_SUCCESS, roll3, trybots3, true, roll2, trybots2, true)

	// The successful dry run will not have the commit bit set. Make sure
	// that, when we switch back into normal mode, we re-set the commit bit
	// instead of closing the roll and opening a new one.
	rv.rollerWillSwitchDryRun(roll3, trybots3, false)
	assert.NoError(t, roller.SetMode(autoroll_modes.MODE_RUNNING, u, "Normal mode."))
	checkStatus(t, roller, rv, rm, STATUS_IN_PROGRESS, roll3, trybots3, false, roll2, trybots2, true)
}

// TestAutoRollCommitDescRace ensures that we correctly handle the case in which
// a roll CL lands but is not yet updated with the "Committed: ..." string in
// the CL description when the roller sees it next. In this case, we expect the
// roller to query the commit queue directly to determine whether it landed the
// CL.
func TestAutoRollCommitDescRace(t *testing.T) {
	workdir, roller, rm, rv, roll1 := setup(t)
	defer func() {
		assert.NoError(t, roller.Close())
		assert.NoError(t, os.RemoveAll(workdir))
	}()

	trybot := &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Status:           autoroll.TRYBOT_STATUS_COMPLETED,
		Result:           autoroll.TRYBOT_RESULT_SUCCESS,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq\"}",
	}
	trybots := []*buildbucket.Build{trybot}

	// Pretend that the roll landed BUT the CL description was not updated.

	// Determine what revision we rolled to.
	m := autoroll.ROLL_REV_REGEX.FindStringSubmatch(roll1.Subject)
	assert.NotNil(t, m)
	assert.Equal(t, 3, len(m))
	rolledTo, err := rm.FullChildHash(m[2])
	assert.NoError(t, err)
	rm.mockRolledPast(rolledTo, true)
	rm.mockLastRollRev(rolledTo)
	rm.mockForceUpdate()

	// Fake the roll in Rietveld.
	roll1.Closed = true
	roll1.CommitQueue = false
	roll1.CommitQueueDryRun = false
	now := time.Now().UTC().Round(time.Millisecond)
	roll1.Modified = now
	roll1.ModifiedString = now.Format(rietveld.TIME_FORMAT)
	url := fmt.Sprintf("%s/api/%d?messages=true", autoroll.RIETVELD_URL, roll1.Issue)
	serialized, err := json.Marshal(roll1)
	assert.NoError(t, err)
	rv.urlMock.MockOnce(url, mockhttpclient.MockGetDialogue(serialized))
	rv.mockTrybotResults(roll1, trybots)

	// Fake the CQ status URL.
	cqUrl := fmt.Sprintf(rietveld.CQ_STATUS_URL, roll1.Issue, roll1.Patchsets[len(roll1.Patchsets)-1])
	rv.urlMock.MockOnce(cqUrl, mockhttpclient.MockGetDialogue([]byte(fmt.Sprintf("{\"success\":%v}", true))))

	// Run the roller.
	assert.NoError(t, roller.doAutoRoll())

	// Verify that the roller correctly determined that the CL landed.
	roll1.Committed = true
	checkStatus(t, roller, rv, rm, STATUS_UP_TO_DATE, nil, nil, false, roll1, trybots, false)
}

// TestAutoRollCommitLandRace ensures that we correctly handle the case in which
// a roll CL succeeds, is closed by the CQ, but does not show up in the repo by
// the time we check for it. In this case, we expect the roller to repeatedly
// sync the code, waiting for the commit to show up.
func TestAutoRollCommitLandRace(t *testing.T) {
	workdir, roller, rm, rv, roll1 := setup(t)
	defer func() {
		assert.NoError(t, roller.Close())
		assert.NoError(t, os.RemoveAll(workdir))
	}()

	// Pretend the roll landed but has not yet showed up in the repo.
	trybot := &buildbucket.Build{
		CreatedTimestamp: fmt.Sprintf("%d", time.Now().UTC().UnixNano()/1000000),
		Status:           autoroll.TRYBOT_STATUS_COMPLETED,
		Result:           autoroll.TRYBOT_RESULT_SUCCESS,
		ParametersJson:   "{\"builder_name\":\"fake-builder\",\"category\":\"cq\"}",
	}
	trybots := []*buildbucket.Build{trybot}

	roll1.Closed = true
	roll1.Committed = true
	roll1.CommitQueue = false
	roll1.CommitQueueDryRun = false
	roll1.Description += "\n" + COMMITTED_STR
	rv.modify(roll1, trybots)

	// The repo will have to force update multiple times.
	rm.mockForceUpdate()
	rm.mockForceUpdate()
	rm.mockForceUpdate()
	// This goroutine will cause the CL to "land" after a couple of tries.
	go func() {
		for {
			if rm.getForceUpdateCount() == 0 {
				m := autoroll.ROLL_REV_REGEX.FindStringSubmatch(roll1.Subject)
				assert.NotNil(t, m)
				assert.Equal(t, 3, len(m))
				rolledTo, err := rm.FullChildHash(m[2])
				assert.NoError(t, err)
				rm.mockRolledPast(rolledTo, true)
				rm.mockLastRollRev(rolledTo)
				rm.mockForceUpdate()
				return

			}
			time.Sleep(time.Second)
		}
	}()

	// Run the roller.
	assert.NoError(t, roller.doAutoRoll())
	checkStatus(t, roller, rv, rm, STATUS_UP_TO_DATE, nil, nil, false, roll1, trybots, false)
}
