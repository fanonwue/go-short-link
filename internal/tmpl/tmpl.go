package tmpl

import (
	"html/template"
	"io/fs"
	"path"
	"strings"
)

type (
	TemplateParserContext struct {
		baseTemplate *template.Template
		funcMap      template.FuncMap
	}
)

const templatePathPrefix = "html/"
const BaseTemplateName = "base.gohtml"

var (
	templates fs.FS
)

func (tpc *TemplateParserContext) ParseTemplate(templateName string, templateData string) (*template.Template, error) {
	return readTemplateWithBaseFuncMap(tpc.baseTemplate, templateName, templateData, tpc.funcMap)
}

func (tpc *TemplateParserContext) ParseTemplateFile(templateName string) (*template.Template, error) {
	return readTemplateWithBaseFuncMap(tpc.baseTemplate, templateName, "", tpc.funcMap)
}

func (tpc *TemplateParserContext) ParseBaseTemplateFile(templateName string) (*template.Template, error) {
	base, err := tpc.ParseTemplateFile(templateName)
	if err != nil {
		return nil, err
	}
	tpc.SetBaseTemplate(base)
	return base, nil
}

func (tpc *TemplateParserContext) ParseBaseTemplate(templateName string, templateData string) (*template.Template, error) {
	base, err := tpc.ParseTemplate(templateName, templateData)
	if err != nil {
		return nil, err
	}
	tpc.SetBaseTemplate(base)
	return base, nil
}

func (tpc *TemplateParserContext) SetBaseTemplate(template *template.Template) {
	tpc.baseTemplate = template
}

func (tpc *TemplateParserContext) SetFuncMap(funcMap template.FuncMap) {
	tpc.funcMap = funcMap
}

func NewTemplateParserContext() *TemplateParserContext {
	return &TemplateParserContext{
		baseTemplate: nil,
		funcMap:      nil,
	}
}

func TemplateFS() fs.FS {
	return templates
}

func TemplatePath(templateName string) string {
	if strings.HasPrefix(templateName, templatePathPrefix) {
		return templateName
	}
	return path.Join(templatePathPrefix, templateName)
}

func readTemplateWithBaseFuncMap(
	baseTemplate *template.Template,
	templateName string,
	templateData string,
	funcMap template.FuncMap,
) (*template.Template, error) {
	return readTemplateWithBaseCallback(baseTemplate, templateName, templateData, func(createdTemplate *template.Template) *template.Template {
		if funcMap == nil {
			return createdTemplate
		}
		return createdTemplate.Funcs(funcMap)
	})
}

func readTemplateWithBaseCallback(
	baseTemplate *template.Template,
	templateName string,
	templateData string,
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

	if templateData != "" {
		return base.Parse(templateData)
	}

	return base.ParseFS(TemplateFS(), TemplatePath(templateName))
}
