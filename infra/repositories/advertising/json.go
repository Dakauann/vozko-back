package advertising_repository

import (
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/datatypes"
)

func encodeJSON(v any) (datatypes.JSON, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	return datatypes.JSON(raw), nil
}

func decodeJSON(raw datatypes.JSON, into any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, into)
}

func splitList(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

var (
	errMetaAccountRequired       = errors.New("advertising repository: meta account id is required")
	errAccountRequired           = errors.New("advertising repository: ad account id is required")
	errForeignObject             = errors.New("advertising repository: object does not belong to the account level being replaced")
	errForeignInsight            = errors.New("advertising repository: insight row does not belong to the account and range being replaced")
	errMetaIDRequired            = errors.New("advertising repository: meta id is required")
	errFormTrackedElsewhere      = errors.New("advertising repository: lead form is already tracked by another workspace")
	errFormLeadNotFound          = errors.New("advertising repository: form lead not found")
	errSettingsNotSaved          = errors.New("advertising repository: conversion settings were not saved")
	errConversionKeyRequired     = errors.New("advertising repository: opportunity id and event name are required")
	errConversionRecordElsewhere = errors.New("advertising repository: conversion record belongs to another workspace")
)
