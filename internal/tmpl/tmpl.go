package tmpl

import (
	"io/fs"
	"path"
)

const templatePathPrefix = "./html/"
const BaseTemplateName = "base.gohtml"

var (
	templates fs.FS
)

func TemplateFS() fs.FS {
	return templates
}

func TemplatePath(templateName string) string {
	return path.Join(templatePathPrefix, templateName)
}
