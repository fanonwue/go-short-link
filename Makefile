# Add linker flags when building for production
# those will strip symbols and debug info
# both "prod" and "production" count as production target
PROD_TARGETS=prod production
ifneq (, $(filter $(TARGET), $(PROD_TARGETS)))
	LD_FLAGS := -w -s
endif

EXECUTABLE_NAME := go-short-link
ifeq ($(GOOS), windows)
	EXECUTABLE_NAME := $(EXECUTABLE_NAME).exe
endif

build:
	go build -o bin/$(EXECUTABLE_NAME) --ldflags="$(LD_FLAGS)"

deps:
	go mod download && go mod verify

deps-update:
	go get -u && go mod tidy