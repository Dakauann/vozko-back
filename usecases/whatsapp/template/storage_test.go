package template_usecase

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/whatsapp/template"
)

type anyStorage struct{}

func (anyStorage) KeyFromURL(url string) (string, bool) { return url, true }

type ourStorage struct{}

func (ourStorage) KeyFromURL(url string) (string, bool) {
	return strings.TrimPrefix(url, "https://files.test/"), strings.HasPrefix(url, "https://files.test/")
}

func TestCreateTemplate_Meta_RefusesAHeaderURLOutsideOurStorage(t *testing.T) {
	client := &mediaHeaderMockClient{wantsURL: false}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	uc := NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{}, ourStorage{})

	_, err := uc.Execute(baseCreateInput(mediaHeaderComponent("http://169.254.169.254/latest/meta-data"), positionalBodyComponent()))
	if !errors.Is(err, template.ErrHeaderMediaOutsideStorage) || client.uploadCalls != 0 {
		t.Fatalf("err %v uploads %d", err, client.uploadCalls)
	}
}

func TestCreateTemplate_Meta_UploadsAHeaderFromOurStorage(t *testing.T) {
	client := &mediaHeaderMockClient{wantsURL: false}
	factory := &sendMockClientFactory{client: client, wabaID: "waba-1"}
	uc := NewCreateTemplateUseCase(factory, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{}, ourStorage{})

	if _, err := uc.Execute(baseCreateInput(mediaHeaderComponent("https://files.test/ws1/banner.jpg"), positionalBodyComponent())); err != nil || client.uploadCalls != 1 {
		t.Fatalf("err %v uploads %d", err, client.uploadCalls)
	}
}
