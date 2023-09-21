LD_FLAGS_PROD := -w -s

# Add linker flags when building for production
# those will strip symbols and debug info
# both "prod" and "production" count as production target
ifeq ($(TARGET), prod)
LD_FLAGS := $(LD_FLAGS_PROD)
endif

ifeq ($(TARGET), production)
LD_FLAGS := $(LD_FLAGS_PROD)
endif

build:
	go build -o bin/go-short-link --ldflags="$(LD_FLAGS)"