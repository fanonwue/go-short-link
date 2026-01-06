TRUTHY_VALUES=true yes 1

# Add linker flags when building for production
# those will strip symbols and debug info
# both "prod" and "production" count as production target
PROD_TARGETS=prod production
ifneq (, $(filter $(TARGET), $(PROD_TARGETS)))
	GO_TAGS := release
	LD_FLAGS := -w -s
	ADDITIONAL_FLAGS := -trimpath
else
	GO_TAGS := debug
endif

ifneq (, $(filter $(NO_EMBED), $(TRUTHY_VALUES)))
	GO_TAGS := $(GO_TAGS),noembed
endif

ifneq (, $(filter $(MINIFY), $(TRUTHY_VALUES)))
	GO_TAGS := $(GO_TAGS),minify
endif

EXECUTABLE_NAME := go-short-link
ifeq ($(GOOS), windows)
	EXECUTABLE_NAME := $(EXECUTABLE_NAME).exe
endif

# set default amd64 architecture level
# see https://en.wikipedia.org/wiki/X86-64#Microarchitecture_levels
GOAMD64 ?= v2

BUILD_DATE ?= $(shell date '+%Y-%m-%dT%H:%M:%S%z')
LD_FLAGS := $(LD_FLAGS) -X 'github.com/fanonwue/go-short-link/internal/build.Timestamp=$(BUILD_DATE)'

build:
	go build -tags $(GO_TAGS) -o bin/$(EXECUTABLE_NAME) --ldflags="$(LD_FLAGS)" $(ADDITIONAL_FLAGS)

build-noembed:
	go build -tags $(GO_TAGS),noembed -o bin/$(EXECUTABLE_NAME) --ldflags="$(LD_FLAGS)" $(ADDITIONAL_FLAGS)

deps:
	go mod download && go mod verify

deps-update:
	go get -u && go mod tidy