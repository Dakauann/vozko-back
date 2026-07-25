package notification_service

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"vozko/brand"
)

// emailFuncs are available inside every email template. dict lets a template pass
// named arguments to a shared component (e.g. the CTA button); safeHTML injects a
// pre-built SVG glyph without escaping.
var emailFuncs = template.FuncMap{
	"dict": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("dict: needs an even number of arguments")
		}
		m := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict: keys must be strings")
			}
			m[key] = values[i+1]
		}
		return m, nil
	},
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
}

const (
	emailLayoutFile     = "_layout.html"
	emailComponentsFile = "_components.html"
)

type TemplateLoaderService struct {
	templates    map[string]*template.Template
	templatesDir string
}

func NewTemplateLoaderService(templatesDir string) *TemplateLoaderService {
	return &TemplateLoaderService{
		templates:    make(map[string]*template.Template),
		templatesDir: templatesDir,
	}
}

func (tls *TemplateLoaderService) LoadTemplate(filePath string, data map[string]interface{}) (string, error) {
	// Inject the active white-label brand so every template (and the shared layout)
	// can render brand chrome via {{.Brand.Name}}, {{.Brand.LogoURL}}, etc. No email
	// template carries a hardcoded brand.
	if data == nil {
		data = map[string]interface{}{}
	}
	if _, exists := data["Brand"]; !exists {
		data["Brand"] = brand.Active()
	}

	templatesDir := tls.resolveTemplatesDir()
	var fullPath string
	if filepath.IsAbs(filePath) {
		fullPath = filePath
	} else {
		fullPath = filepath.Join(templatesDir, filePath)
	}

	tmplBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file %s: %w", fullPath, err)
	}

	// Templates that define a "content" block render inside the shared branded
	// layout (header, footer, design-system styles, reusable components). Legacy
	// standalone templates that do not are still rendered on their own, so the
	// migration is incremental and nothing breaks mid-conversion.
	if strings.Contains(string(tmplBytes), `{{define "content"}}`) {
		return tls.renderWithLayout(templatesDir, fullPath, data)
	}

	tmpl, err := template.New(filepath.Base(fullPath)).Funcs(emailFuncs).Parse(string(tmplBytes))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", fullPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", fullPath, err)
	}

	return buf.String(), nil
}

func (tls *TemplateLoaderService) renderWithLayout(templatesDir, contentPath string, data map[string]interface{}) (string, error) {
	layoutPath := filepath.Join(templatesDir, emailLayoutFile)
	componentsPath := filepath.Join(templatesDir, emailComponentsFile)

	tmpl, err := template.New(emailLayoutFile).Funcs(emailFuncs).ParseFiles(layoutPath, componentsPath, contentPath)
	if err != nil {
		return "", fmt.Errorf("failed to parse email layout for %s: %w", contentPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, emailLayoutFile, data); err != nil {
		return "", fmt.Errorf("failed to execute email layout for %s: %w", contentPath, err)
	}
	return buf.String(), nil
}
func (tls *TemplateLoaderService) resolveTemplatesDir() string {
	dir := tls.templatesDir
	if strings.TrimSpace(dir) == "" {
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, "infra", "notifications", "templates")
	}
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if filepath.IsAbs(dir) {

		stripped := strings.TrimLeft(dir, string(os.PathSeparator)+"/\\")
		if stripped != dir {
			cwd, _ := os.Getwd()
			candidate := filepath.Join(cwd, stripped)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	cwd, _ := os.Getwd()
	candidate := filepath.Join(cwd, dir)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return dir
}
