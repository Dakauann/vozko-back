package template

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var ErrParameterNameInvalid = errors.New("template: a named variable uses only lowercase letters, digits and underscores, starting with a letter")

type ParameterNameError struct{ Name string }

func (e *ParameterNameError) Error() string {
	return fmt.Sprintf("%s: {{%s}}", ErrParameterNameInvalid.Error(), e.Name)
}

func (e *ParameterNameError) Unwrap() error { return ErrParameterNameInvalid }

var parameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func CheckParameterName(name string) error {
	if !parameterNamePattern.MatchString(name) {
		return &ParameterNameError{Name: name}
	}
	return nil
}

func CheckParameterNames(names []string) error {
	for _, name := range names {
		if err := CheckParameterName(name); err != nil {
			return err
		}
	}
	return nil
}

func checkComponentParameterNames(components []TemplateComponent) error {
	for _, c := range components {
		kind := strings.ToUpper(c.Type)
		if kind != "BODY" && !(kind == "HEADER" && strings.ToUpper(c.Format) == "TEXT") {
			continue
		}
		for _, name := range extractTemplateParameters(c.Text) {
			if _, err := strconv.Atoi(name); err == nil {
				continue
			}
			if err := CheckParameterName(name); err != nil {
				return err
			}
		}
	}
	return nil
}
