package tmpl

import (
	"io/fs"
	"path"
)

const templatePathPrefix = "./html/"
const BaseTemplateName = "base.gohtml"

var (
	templates   fs.FS
	initialized = false
)

func TemplateFS() fs.FS {
	initialize()
	return templates
}

func TemplatePath(templateName string) string {
	return path.Join(templatePathPrefix, templateName)
}
