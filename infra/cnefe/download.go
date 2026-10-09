package cnefe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	partSuffix      = ".part"
	BaseURL         = "https://ftp.ibge.gov.br/Cadastro_Nacional_de_Enderecos_para_Fins_Estatisticos/Censo_Demografico_2022/Arquivos_CNEFE/CSV/UF/"
	MunicipalityURL = "https://servicodados.ibge.gov.br/api/v1/localidades/municipios?view=nivelado"
	defaultRetries  = 5
	firstRetryWait  = 2 * time.Second
)

var errUnexpectedStatus = errors.New("cnefe: unexpected download status")

type Downloader struct {
	Client  *http.Client
	Retries int
	Wait    func(ctx context.Context, d time.Duration) error
}

func NewDownloader(client *http.Client) *Downloader {
	return &Downloader{Client: client, Retries: defaultRetries, Wait: waitFor}
}

func waitFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Downloader) Fetch(ctx context.Context, url, dest string) error {
	if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
		return nil
	}
	var last error
	for attempt := 0; attempt <= max(d.Retries, 0); attempt++ {
		if attempt > 0 {
			if err := d.Wait(ctx, firstRetryWait<<min(attempt-1, 5)); err != nil {
				return err
			}
		}
		done, err := d.resume(ctx, url, dest+partSuffix)
		if err == nil && done {
			return os.Rename(dest+partSuffix, dest)
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("cnefe: download %s: %w", url, last)
}

func (d *Downloader) resume(ctx context.Context, url, part string) (bool, error) {
	offset := int64(0)
	if info, err := os.Stat(part); err == nil {
		offset = info.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	expected := resp.ContentLength
	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable:
		total, ok := totalOf(resp.Header.Get("Content-Range"))
		return ok && total == offset, nil
	case http.StatusPartialContent:
		flags |= os.O_APPEND
		if expected >= 0 {
			expected += offset
		}
	case http.StatusOK:
		flags |= os.O_TRUNC
	default:
		return false, fmt.Errorf("%w: %d", errUnexpectedStatus, resp.StatusCode)
	}
	file, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return false, err
	}
	written, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if resp.StatusCode == http.StatusPartialContent {
		written += offset
	}
	if expected >= 0 && written != expected {
		return false, fmt.Errorf("cnefe: download stopped at %d of %d bytes", written, expected)
	}
	return true, nil
}

func totalOf(contentRange string) (int64, bool) {
	_, total, found := strings.Cut(contentRange, "/")
	if !found {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	return value, err == nil
}
