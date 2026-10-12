package library

import (
	"regexp"
	"testing"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestPictureItemsCarryTheFileTheyImport(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range c.Items {
		if item.Kind != KindAsset {
			if item.Asset != nil {
				t.Errorf("item %s is not a picture but carries a file", item.ID)
			}
			continue
		}
		count++
		asset := item.Asset
		if asset == nil || !sha256Pattern.MatchString(asset.SHA256) || asset.Width <= 0 || asset.Height <= 0 || asset.ContentType != "image/webp" || asset.Path == "" {
			t.Errorf("item %s has a broken file %+v", item.ID, asset)
		}
		if item.Source.License != "CC0-1.0" && item.Source.License != "MIT" {
			t.Errorf("item %s has license %q", item.ID, item.Source.License)
		}
	}
	if count < 100 {
		t.Fatalf("the catalog holds %d 3D pictures", count)
	}
}

func TestMockupsFrameTheSelectedClipInVideo(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	phone, ok := c.Find("mockup-phone")
	if !ok || phone.Kind != KindMockup || !phone.Frames() || !phone.Supports("image") || !phone.Supports("video") {
		t.Fatalf("phone = %+v", phone)
	}
}
