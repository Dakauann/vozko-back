package ses

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"vozko/domain/emailsending"
)

type fakeObjects struct {
	body      []byte
	length    int64
	input     *s3.GetObjectInput
	deleted   []string
	deleteErr error
	err       error
}

func (f *fakeObjects) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.input = in
	if f.err != nil {
		return nil, f.err
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body)), ContentLength: aws.Int64(f.length)}, nil
}

func TestRawMessageStoreFetch(t *testing.T) {
	objects := &fakeObjects{body: []byte("From: a@b.com\r\n\r\nhi"), length: 19}
	store, err := NewRawMessageStore(objects, "inbound-bucket")
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Fetch(context.Background(), "inbound/abc")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "From: a@b.com\r\n\r\nhi" || aws.ToString(objects.input.Bucket) != "inbound-bucket" || aws.ToString(objects.input.Key) != "inbound/abc" {
		t.Fatalf("data = %q input = %+v", data, objects.input)
	}
}

func TestRawMessageStoreGuards(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		objects *fakeObjects
		want    error
	}{
		{"key outside prefix", "other/abc", &fakeObjects{}, emailsending.ErrRawMessageKey},
		{"traversal", "inbound/../x", &fakeObjects{}, emailsending.ErrRawMessageKey},
		{"declared too large", "inbound/abc", &fakeObjects{length: emailsending.MaxRawMessageBytes + 1}, emailsending.ErrRawMessageTooLarge},
		{"body larger than declared", "inbound/abc", &fakeObjects{body: make([]byte, emailsending.MaxRawMessageBytes+1), length: 10}, emailsending.ErrRawMessageTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := NewRawMessageStore(tc.objects, "b")
			if _, err := store.Fetch(context.Background(), tc.key); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	store, _ := NewRawMessageStore(&fakeObjects{err: errors.New("denied")}, "b")
	if _, err := store.Fetch(context.Background(), "inbound/abc"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NewRawMessageStore(nil, "b"); err == nil {
		t.Fatal("nil client must fail")
	}
}

func (f *fakeObjects) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.Bucket)+"/"+aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, f.deleteErr
}

func TestRawMessageStoreDelete(t *testing.T) {
	objects := &fakeObjects{}
	store, err := NewRawMessageStore(objects, "inbound-bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "inbound/abc"); err != nil {
		t.Fatal(err)
	}
	if len(objects.deleted) != 1 || objects.deleted[0] != "inbound-bucket/inbound/abc" {
		t.Fatalf("deleted = %v", objects.deleted)
	}
	for _, key := range []string{"other/abc", "inbound/../secrets", "inbound/"} {
		if err := store.Delete(context.Background(), key); !errors.Is(err, emailsending.ErrRawMessageKey) {
			t.Fatalf("delete %q = %v, only inbound objects may be deleted", key, err)
		}
	}
	if len(objects.deleted) != 1 {
		t.Fatalf("a refused key reached the bucket: %v", objects.deleted)
	}
	objects.deleteErr = errors.New("access denied")
	if err := store.Delete(context.Background(), "inbound/abc"); err == nil {
		t.Fatal("a failed delete must be reported")
	}
}
