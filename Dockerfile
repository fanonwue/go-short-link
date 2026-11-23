ARG WORKDIR=/opt/app
ARG TMPDIR=/tmp/app
ARG UID=3000
ARG GID=3000

FROM golang:1.27-alpine AS builder
ARG UID
ARG GID
ARG BUILDDIR=/opt/build
ARG WORKDIR
ARG TMPDIR
# Set Target to production for Makefile
ENV TARGET=prod
# CGO should be disabled anyway because no build tools are installed, but this makes sure the resulting binary
# will be a static binary
ENV CGO_ENABLED=0
WORKDIR $BUILDDIR

RUN mkdir $TMPDIR && chown $UID:$GID $TMPDIR

# make is needed for the Makefile
RUN apk add --no-cache \
    ca-certificates-bundle \
    tzdata \
    make

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum Makefile ./
RUN --mount=type=cache,target=/go/pkg/mod \
    make deps

COPY . .
# Run go build and strip symbols / debug info
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make build

WORKDIR $WORKDIR
RUN mkdir bin && mkdir data \
    && cp $BUILDDIR/bin/go-short-link bin/ \
    && chown $UID:$GID data

FROM scratch
ARG UID
ARG GID
ARG WORKDIR
ARG TMPDIR
ENV APP_ENV=production
ENV TMPDIR=$TMPDIR
WORKDIR $WORKDIR

COPY --from=builder /tmp/. /tmp
# Copy CA certificates from the builder so that they are available to the application
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Uncomment this if you need timezone information in the image
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder $WORKDIR/. .

EXPOSE 3000
USER $UID:$GID

ENTRYPOINT ["./bin/go-short-link"]