package tmpl

import (
	"html/template"
	"io/fs"
	"path"
	"strings"
)

const templatePathPrefix = "html/"
const BaseTemplateName = "base.gohtml"

var (
	templates fs.FS
)

func TemplateFS() fs.FS {
	return templates
}

func TemplatePath(templateName string) string {
	if strings.HasPrefix(templateName, templatePathPrefix) {
		return templateName
	}
	return path.Join(templatePathPrefix, templateName)
}

func ReadTemplate(templateName string, funcMap template.FuncMap) (*template.Template, error) {
	return ReadTemplateWithBaseFuncMap(nil, templateName, funcMap)
}

func ReadTemplateWithBase(baseTemplate *template.Template, templateName string) (*template.Template, error) {
	return ReadTemplateWithBaseCallback(baseTemplate, templateName, nil)
}

func ReadTemplateWithBaseFuncMap(baseTemplate *template.Template, templateName string, funcMap template.FuncMap) (*template.Template, error) {
	return ReadTemplateWithBaseCallback(baseTemplate, templateName, func(createdTemplate *template.Template) *template.Template {
		if funcMap == nil {
			return createdTemplate
		}
		return createdTemplate.Funcs(funcMap)
	})
}

func ReadTemplateWithBaseCallback(
	baseTemplate *template.Template,
	templateName string,
	preParseCallback func(createdTemplate *template.Template) *template.Template,
) (*template.Template, error) {
	var base *template.Template
	if baseTemplate == nil {
		base = template.New(templateName)
	} else {
		cloned, err := baseTemplate.Clone()
		if err != nil {
			return nil, err
		}
		base = cloned
	}

	if preParseCallback != nil {
		base = preParseCallback(base)
	}

	return base.ParseFS(TemplateFS(), TemplatePath(templateName))
}
