package campaignaudience_usecase

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	report_xlsx "vozko/usecases/report/xlsx"
)

type SampleFormat string

const (
	SampleCSV  SampleFormat = "csv"
	SampleXLSX SampleFormat = "xlsx"
)

var ErrSampleFormat = errors.New("campaign audience: the sample is a csv or an xlsx file")

const (
	sampleBaseName = "modelo-publico"
	sampleSheet    = "Modelo"
	samplePhone    = "5511999990000"
	sampleName     = "Maria Silva"
	sampleEmail    = "maria@exemplo.com.br"
	sampleCustom   = "exemplo"
)

var sampleValues = map[campaign.BindingSource]string{
	campaign.BindFirstName: "Maria",
	campaign.BindName:      sampleName,
	campaign.BindNickname:  "Mari",
	campaign.BindDistrict:  "Centro",
	campaign.BindCity:      "São Paulo",
	campaign.BindOwnerName: "Ana",
}

type File struct {
	Name        string
	ContentType string
	Data        []byte
}

func (s *Service) Sample(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, vars []campaignaudience.Variable, format SampleFormat) (File, error) {
	if format == "" {
		format = SampleCSV
	}
	if format != SampleCSV && format != SampleXLSX {
		return File{}, ErrSampleFormat
	}
	if err := listChannel(ch); err != nil {
		return File{}, err
	}
	header, example := []string{"email", "name"}, []string{sampleEmail, sampleName}
	if ch.Contact() == campaign.ContactPhone {
		var err error
		if header, example, err = s.phoneSample(ctx, a, ch, params, vars); err != nil {
			return File{}, err
		}
	}
	if format == SampleXLSX {
		var buf bytes.Buffer
		if err := report_xlsx.WriteTable(&buf, sampleSheet, header, [][]string{example}); err != nil {
			return File{}, err
		}
		return File{Name: sampleBaseName + ".xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: buf.Bytes()}, nil
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	if err := w.WriteAll([][]string{header, example}); err != nil {
		return File{}, err
	}
	return File{Name: sampleBaseName + ".csv", ContentType: "text/csv; charset=utf-8", Data: buf.Bytes()}, nil
}

func (s *Service) phoneSample(ctx context.Context, a Actor, ch campaignaudience.Channel, params Params, vars []campaignaudience.Variable) ([]string, []string, error) {
	channel, err := s.channel(ch)
	if err != nil {
		return nil, nil, err
	}
	prepared, err := channel.Prepare(ctx, a, params)
	if err != nil {
		return nil, nil, err
	}
	bound, err := campaignaudience.Bind(prepared.Slots, vars)
	if err != nil {
		return nil, nil, err
	}
	defs, err := s.definitions(a.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	header := []string{"number", "name"}
	example := []string{samplePhone, sampleName}
	for i, b := range bound {
		n := i + 1
		column := campaign.FieldColumn(b.Source, defs)
		if column == "" {
			column = "var" + strconv.Itoa(n)
		}
		header = append(header, column)
		example = append(example, sampleValue(b, prepared.Slots[i], n))
	}
	return header, example, nil
}

func sampleValue(b campaignaudience.Bound, slot campaignaudience.Slot, n int) string {
	switch {
	case slot.Example != "":
		return slot.Example
	case b.Source == campaign.BindLiteral && b.Value != "":
		return b.Value
	case sampleValues[b.Source] != "":
		return sampleValues[b.Source]
	case b.Source != "":
		return sampleCustom
	}
	return "valor " + strconv.Itoa(n)
}
