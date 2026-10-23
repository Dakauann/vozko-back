package emailinbox

import "testing"

func sized(name string, size int64) Attachment {
	return Attachment{FileName: name, Data: make([]byte, 1), Size: size}
}

func TestAttachmentsWithinBudget(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		name string
		in   []Attachment
		want []bool
	}{
		{name: "small files are all kept", in: []Attachment{sized("a", mb), sized("b", 2*mb)}, want: []bool{true, true}},
		{name: "the total, not each file, is capped at 25 MB", in: []Attachment{sized("a", 10*mb), sized("b", 10*mb), sized("c", 10*mb)}, want: []bool{true, true, false}},
		{name: "a later small file still fits after a dropped big one", in: []Attachment{sized("a", 20*mb), sized("b", 10*mb), sized("c", 5*mb)}, want: []bool{true, false, true}},
		{name: "exactly the budget fits", in: []Attachment{sized("a", MaxAttachmentTotalBytes)}, want: []bool{true}},
		{name: "a file read without its data is never kept", in: []Attachment{{FileName: "big", Size: MaxAttachmentTotalBytes + 1}}, want: []bool{false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AttachmentsWithinBudget(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
	if MaxAttachmentTotalBytes != 25<<20 {
		t.Fatalf("the kept attachments budget is %d bytes, want 25 MB", MaxAttachmentTotalBytes)
	}
}
