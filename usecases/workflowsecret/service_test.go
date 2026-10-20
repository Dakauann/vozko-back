package workflowsecret_usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/workflowsecret"
)

type fakeRepo struct {
	upserted    []workflowsecret.Secret
	deleted     []string
	stored      map[string]workflowsecret.Secret
	findCalls   [][]string
	summaries   []workflowsecret.Summary
	deleteError error
}

func (r *fakeRepo) Upsert(_ context.Context, s workflowsecret.Secret) (workflowsecret.Secret, error) {
	r.upserted = append(r.upserted, s)
	return s, nil
}

func (r *fakeRepo) Delete(_ context.Context, workspaceID, name string) error {
	r.deleted = append(r.deleted, workspaceID+"/"+name)
	return r.deleteError
}

func (r *fakeRepo) List(context.Context, string) ([]workflowsecret.Summary, error) {
	return r.summaries, nil
}

func (r *fakeRepo) FindByNames(_ context.Context, workspaceID string, names []string) ([]workflowsecret.Secret, error) {
	r.findCalls = append(r.findCalls, names)
	var out []workflowsecret.Secret
	for _, n := range names {
		if s, ok := r.stored[workspaceID+"/"+n]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

type prefixCipher struct{ fail bool }

func (c prefixCipher) Encrypt(p []byte) ([]byte, error) {
	if c.fail {
		return nil, errors.New("kek down")
	}
	return append([]byte("enc:"), p...), nil
}

func (c prefixCipher) Decrypt(ct []byte) ([]byte, error) {
	if c.fail || !strings.HasPrefix(string(ct), "enc:") {
		return nil, errors.New("bad envelope")
	}
	return ct[4:], nil
}

var fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T, repo *fakeRepo, cipher workflowsecret.Cipher) *Service {
	t.Helper()
	s, err := New(Deps{Secrets: repo, Cipher: cipher, Now: func() time.Time { return fixedNow }, NewID: func() string { return "id-1" }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	if _, err := New(Deps{Cipher: prefixCipher{}}); err == nil {
		t.Fatal("missing repository must fail")
	}
	if _, err := New(Deps{Secrets: &fakeRepo{}}); err == nil {
		t.Fatal("missing cipher must fail")
	}
}

func TestPutStoresOnlyTheEncryptedValueAndReportsTheAuthor(t *testing.T) {
	repo := &fakeRepo{}
	got, err := newService(t, repo, prefixCipher{}).Put(context.Background(), "ws", "u1", "unifecaf_senha", "p4ss")
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.upserted) != 1 {
		t.Fatalf("upserts: %+v", repo.upserted)
	}
	s := repo.upserted[0]
	if string(s.EncryptedValue) != "enc:p4ss" || s.WorkspaceID != "ws" || s.Name != "unifecaf_senha" || s.ID != "id-1" {
		t.Fatalf("stored %+v", s)
	}
	if s.CreatedBy != "u1" || s.UpdatedBy != "u1" || !s.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("authorship %+v", s)
	}
	if got.Name != "unifecaf_senha" || got.UpdatedBy != "u1" {
		t.Fatalf("summary %+v", got)
	}
}

func TestPutRefusesInvalidInputBeforeTouchingStorage(t *testing.T) {
	cases := []struct {
		name, workspace, secret, value string
		want                           error
	}{
		{"bad name", "ws", "Bad-Name", "v", workflowsecret.ErrInvalidName},
		{"empty value", "ws", "ok", "", workflowsecret.ErrValueRequired},
		{"huge value", "ws", "ok", strings.Repeat("x", workflowsecret.MaxValueBytes+1), workflowsecret.ErrValueTooLong},
		{"no workspace", " ", "ok", "v", workflowsecret.ErrWorkspaceRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			_, err := newService(t, repo, prefixCipher{}).Put(context.Background(), c.workspace, "u1", c.secret, c.value)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v want %v", err, c.want)
			}
			if len(repo.upserted) != 0 {
				t.Fatal("nothing may be stored")
			}
		})
	}
}

func TestPutFailsClosedWhenEncryptionFails(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := newService(t, repo, prefixCipher{fail: true}).Put(context.Background(), "ws", "u1", "k", "v"); err == nil {
		t.Fatal("expected an error")
	}
	if len(repo.upserted) != 0 {
		t.Fatal("a value that could not be encrypted must not be stored")
	}
}

func TestDeleteValidatesTheNameAndPassesNotFoundThrough(t *testing.T) {
	repo := &fakeRepo{deleteError: workflowsecret.ErrNotFound}
	svc := newService(t, repo, prefixCipher{})
	if err := svc.Delete(context.Background(), "ws", "Bad Name"); !errors.Is(err, workflowsecret.ErrInvalidName) {
		t.Fatalf("got %v", err)
	}
	if err := svc.Delete(context.Background(), "ws", "k"); !errors.Is(err, workflowsecret.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if !reflect.DeepEqual(repo.deleted, []string{"ws/k"}) {
		t.Fatalf("deleted %v", repo.deleted)
	}
}

func TestListReturnsSummariesOnly(t *testing.T) {
	repo := &fakeRepo{summaries: []workflowsecret.Summary{{Name: "a", UpdatedBy: "u1", UpdatedAt: fixedNow}}}
	got, err := newService(t, repo, prefixCipher{}).List(context.Background(), "ws")
	if err != nil || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := newService(t, repo, prefixCipher{}).List(context.Background(), ""); !errors.Is(err, workflowsecret.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveQueriesOnceWithTheValidUniqueNamesAndDecrypts(t *testing.T) {
	repo := &fakeRepo{stored: map[string]workflowsecret.Secret{
		"ws/login": {Name: "login", EncryptedValue: []byte("enc:ana")},
		"ws/senha": {Name: "senha", EncryptedValue: []byte("enc:p4ss")},
	}}
	got, err := newService(t, repo, prefixCipher{}).Resolve(context.Background(), "ws", []string{"senha", "login", "senha", "Bad Name", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]string{"login": "ana", "senha": "p4ss"}) {
		t.Fatalf("got %v", got)
	}
	if !reflect.DeepEqual(repo.findCalls, [][]string{{"senha", "login", "absent"}}) {
		t.Fatalf("queries %v", repo.findCalls)
	}
}

func TestResolveWithoutNamesNeverQueries(t *testing.T) {
	repo := &fakeRepo{}
	got, err := newService(t, repo, prefixCipher{}).Resolve(context.Background(), "ws", nil)
	if err != nil || len(got) != 0 || len(repo.findCalls) != 0 {
		t.Fatalf("got %v %v %v", got, err, repo.findCalls)
	}
}

func TestResolveFailsClosedWhenAValueCannotBeDecrypted(t *testing.T) {
	repo := &fakeRepo{stored: map[string]workflowsecret.Secret{"ws/k": {Name: "k", EncryptedValue: []byte("garbage")}}}
	if _, err := newService(t, repo, prefixCipher{}).Resolve(context.Background(), "ws", []string{"k"}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := newService(t, repo, prefixCipher{}).Resolve(context.Background(), "", []string{"k"}); !errors.Is(err, workflowsecret.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
}
