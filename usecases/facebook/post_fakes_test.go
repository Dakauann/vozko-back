package facebook

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	fbdomain "vozko/domain/facebook"
)

type fakePostService struct {
	mu          sync.Mutex
	remote      map[string]*fbdomain.RemotePost
	listed      []*fbdomain.RemotePost
	feedPosts   []fbdomain.FeedPostInput
	photos      []fbdomain.PhotoInput
	updates     []fbdomain.PostUpdate
	deleted     []string
	createErr   error
	photoErrs   []error
	deleteErr   error
	nextPostSeq int

	videos       []fbdomain.VideoInput
	sessions     []fbdomain.VideoTarget
	transfers    []string
	transferErrs []error
	finishes     []string
	finishErr    error
	storyPhotos  []string
	statuses     map[string]*fbdomain.VideoStatus
	stories      []*fbdomain.RemoteStory
}

func newFakePostService() *fakePostService {
	return &fakePostService{remote: map[string]*fbdomain.RemotePost{}, statuses: map[string]*fbdomain.VideoStatus{}}
}

func (f *fakePostService) List(context.Context, string, string, fbdomain.PostListKind, int, string) (*fbdomain.Paged[*fbdomain.RemotePost], error) {
	return &fbdomain.Paged[*fbdomain.RemotePost]{Items: f.listed, NextCursor: "next", HasNext: true}, nil
}
func (f *fakePostService) Get(_ context.Context, _, fbPostID string) (*fbdomain.RemotePost, error) {
	if p, ok := f.remote[fbPostID]; ok {
		return p, nil
	}
	return nil, fbdomain.ErrPostNotFound
}
func (f *fakePostService) CreateFeedPost(_ context.Context, fbPageID, _ string, in fbdomain.FeedPostInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.feedPosts = append(f.feedPosts, in)
	if f.createErr != nil {
		return "", f.createErr
	}
	f.nextPostSeq++
	return fbPageID + "_post" + string(rune('0'+f.nextPostSeq)), nil
}
func (f *fakePostService) UploadPhoto(_ context.Context, fbPageID, _ string, in fbdomain.PhotoInput) (*fbdomain.PhotoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.photos = append(f.photos, in)
	if len(f.photoErrs) > 0 {
		err := f.photoErrs[0]
		f.photoErrs = f.photoErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	id := "photo" + string(rune('0'+len(f.photos)))
	result := &fbdomain.PhotoResult{PhotoID: id}
	if in.Published {
		result.PostID = fbPageID + "_" + id
	}
	return result, nil
}
func (f *fakePostService) Update(_ context.Context, _, _ string, in fbdomain.PostUpdate) error {
	f.updates = append(f.updates, in)
	return nil
}
func (f *fakePostService) Delete(_ context.Context, _, id string) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}
func (f *fakePostService) FetchBytes(context.Context, string) ([]byte, string, error) {
	return []byte("img"), "image/jpeg", nil
}
func (f *fakePostService) ListStories(context.Context, string, string) ([]*fbdomain.RemoteStory, error) {
	return f.stories, nil
}
func (f *fakePostService) CreateVideo(_ context.Context, _, _ string, in fbdomain.VideoInput) (string, error) {
	f.videos = append(f.videos, in)
	if f.createErr != nil {
		return "", f.createErr
	}
	return fmt.Sprintf("V%d", len(f.videos)), nil
}
func (f *fakePostService) StartVideoUpload(_ context.Context, _, _ string, target fbdomain.VideoTarget) (*fbdomain.VideoSession, error) {
	f.sessions = append(f.sessions, target)
	id := fmt.Sprintf("R%d", len(f.sessions))
	return &fbdomain.VideoSession{VideoID: id, UploadURL: "https://rupload/" + id}, nil
}
func (f *fakePostService) TransferVideo(_ context.Context, _ string, session fbdomain.VideoSession, fileURL string) error {
	f.transfers = append(f.transfers, session.VideoID+"<-"+fileURL)
	if len(f.transferErrs) > 0 {
		err := f.transferErrs[0]
		f.transferErrs = f.transferErrs[1:]
		return err
	}
	return nil
}
func (f *fakePostService) FinishReel(_ context.Context, fbPageID, _, videoID string, in fbdomain.ReelFinish) (string, error) {
	f.finishes = append(f.finishes, "reel:"+videoID+":"+in.Description)
	if f.finishErr != nil {
		return "", f.finishErr
	}
	return fbPageID + "_" + videoID, nil
}
func (f *fakePostService) FinishVideoStory(_ context.Context, fbPageID, _, videoID string) (string, error) {
	f.finishes = append(f.finishes, "story:"+videoID)
	return fbPageID + "_s" + videoID, nil
}
func (f *fakePostService) CreatePhotoStory(_ context.Context, fbPageID, _, photoID string) (string, error) {
	f.storyPhotos = append(f.storyPhotos, photoID)
	return fbPageID + "_story", nil
}
func (f *fakePostService) VideoStatus(_ context.Context, _, videoID string) (*fbdomain.VideoStatus, error) {
	if st, ok := f.statuses[videoID]; ok {
		return st, nil
	}
	return &fbdomain.VideoStatus{State: "processing"}, nil
}

type fakePosts struct {
	byFBID map[string]*fbdomain.Post
}

func newFakePosts() *fakePosts { return &fakePosts{byFBID: map[string]*fbdomain.Post{}} }

func (f *fakePosts) UpsertMany(_ context.Context, posts []*fbdomain.Post) error {
	for _, p := range posts {
		copied := *p
		if existing, ok := f.byFBID[p.FBPostID]; ok && existing.CreatedByApp {
			copied.CreatedByApp = true
		}
		f.byFBID[p.FBPostID] = &copied
	}
	return nil
}
func (f *fakePosts) Track(_ context.Context, p *fbdomain.Post) error {
	if existing, ok := f.byFBID[p.FBPostID]; ok {
		existing.CreatedByApp = existing.CreatedByApp || p.CreatedByApp
		return nil
	}
	copied := *p
	f.byFBID[p.FBPostID] = &copied
	return nil
}
func (f *fakePosts) FindByFBPostID(_ context.Context, id string) (*fbdomain.Post, error) {
	if p, ok := f.byFBID[id]; ok {
		return p, nil
	}
	return nil, fbdomain.ErrPostNotFound
}
func (f *fakePosts) AppMadeAmong(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if p, ok := f.byFBID[id]; ok && p.CreatedByApp {
			out[id] = true
		}
	}
	return out, nil
}
func (f *fakePosts) SetHidden(_ context.Context, id string, hidden bool) error {
	if p, ok := f.byFBID[id]; ok {
		p.IsHidden = hidden
	}
	return nil
}
func (f *fakePosts) UpdateMessage(_ context.Context, id, message string) error {
	if p, ok := f.byFBID[id]; ok {
		p.Message = message
	}
	return nil
}
func (f *fakePosts) AddCounts(_ context.Context, id string, reactions, comments int) error {
	if p, ok := f.byFBID[id]; ok {
		p.ReactionsCount += reactions
		p.CommentsCount += comments
	}
	return nil
}
func (f *fakePosts) ListByPage(_ context.Context, pageID string, _, _ int) ([]*fbdomain.Post, error) {
	var out []*fbdomain.Post
	for _, p := range f.byFBID {
		if p.PageID == pageID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakePosts) Remove(_ context.Context, id string) error {
	delete(f.byFBID, id)
	return nil
}

type fakeJobs struct {
	mu        sync.Mutex
	byID      map[string]*fbdomain.PublishJob
	saves     int
	stale     []string
	reelCount int64
}

func newFakeJobs() *fakeJobs { return &fakeJobs{byID: map[string]*fbdomain.PublishJob{}} }

func (f *fakeJobs) Create(_ context.Context, job *fbdomain.PublishJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job.ID = "job-" + string(rune('0'+len(f.byID)+1))
	copied := *job
	f.byID[job.ID] = &copied
	return nil
}
func (f *fakeJobs) FindByID(_ context.Context, id string) (*fbdomain.PublishJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.byID[id]; ok {
		copied := *j
		copied.Progress.PhotoIDs = append([]string(nil), j.Progress.PhotoIDs...)
		return &copied, nil
	}
	return nil, fbdomain.ErrPublishJobNotFound
}
func (f *fakeJobs) Save(_ context.Context, job *fbdomain.PublishJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	copied := *job
	copied.Progress.PhotoIDs = append([]string(nil), job.Progress.PhotoIDs...)
	f.byID[job.ID] = &copied
	return nil
}
func (f *fakeJobs) Claim(_ context.Context, id string, from, to fbdomain.JobStatus) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Status != from {
		return false, nil
	}
	j.Status = to
	j.Attempts++
	return true, nil
}
func (f *fakeJobs) ListByPage(_ context.Context, pageID string, status fbdomain.JobStatus, _ int) ([]*fbdomain.PublishJob, error) {
	var out []*fbdomain.PublishJob
	for _, j := range f.byID {
		if j.PageID == pageID && (status == "" || j.Status == status) {
			out = append(out, j)
		}
	}
	return out, nil
}
func (f *fakeJobs) ReleaseStale(context.Context, time.Time, int) ([]string, error) {
	return f.stale, nil
}
func (f *fakeJobs) FindProcessingByVideoID(_ context.Context, videoID string) (*fbdomain.PublishJob, error) {
	for _, j := range f.byID {
		if j.FBObjectID == videoID && j.Status == fbdomain.JobProcessing {
			copied := *j
			return &copied, nil
		}
	}
	return nil, fbdomain.ErrPublishJobNotFound
}
func (f *fakeJobs) ListDueProcessing(_ context.Context, now time.Time, _ int) ([]*fbdomain.PublishJob, error) {
	var out []*fbdomain.PublishJob
	for _, j := range f.byID {
		if j.Status == fbdomain.JobProcessing && j.NextCheckAt != nil && !j.NextCheckAt.After(now) {
			copied := *j
			out = append(out, &copied)
		}
	}
	return out, nil
}
func (f *fakeJobs) CountSince(context.Context, string, fbdomain.PublishKind, time.Time) (int64, error) {
	return f.reelCount, nil
}

type fakeQueue struct {
	enqueued []string
	err      error
}

func (f *fakeQueue) Enqueue(jobID string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, jobID)
	return nil
}

var errTransport = errors.New("connection reset by peer")

func publishingPage() *fbdomain.Page {
	p := messagingPage()
	p.GrantedScopes = append(p.GrantedScopes,
		fbdomain.ScopeManagePosts, fbdomain.ScopeReadEngagement, fbdomain.ScopeReadUserContent)
	p.Tasks = []fbdomain.Task{fbdomain.TaskManage}
	return p
}

func photoRef(url string) fbdomain.MediaRef {
	return fbdomain.MediaRef{URL: url, MIMEType: "image/jpeg", SizeBytes: 1 << 20}
}

func mp4Ref(url string) fbdomain.MediaRef {
	return fbdomain.MediaRef{URL: url, MIMEType: "video/mp4", SizeBytes: 20 << 20}
}
