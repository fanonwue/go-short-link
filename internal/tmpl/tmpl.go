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

func initIfNeeded() {
	if initialized {
		return
	}
	initialize()
	initialized = true
}

func TemplateFS() fs.FS {
	initIfNeeded()
	return templates
}

func TemplatePath(templateName string) string {
	return path.Join(templatePathPrefix, templateName)
}
