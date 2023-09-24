# Add linker flags when building for production
# those will strip symbols and debug info
# both "prod" and "production" count as production target
PROD_TARGETS=prod production
ifneq (, $(filter $(TARGET), $(PROD_TARGETS)))
	LD_FLAGS := -w -s
endif


build:
	go build -o bin/go-short-link --ldflags="$(LD_FLAGS)"