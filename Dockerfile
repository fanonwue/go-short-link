ARG WORKDIR=/opt/app

FROM golang:1.25-alpine AS builder
ARG WORKDIR
# Set Target to production for Makefile
ENV TARGET=prod
# CGO should be disabled anyway because no build tools are installed, but this makes sure the resulting binary
# will be a static binary
ENV CGO_ENABLED=0
WORKDIR $WORKDIR

# make is needed for the Makefile
RUN apk update && apk upgrade && apk add --no-cache \
    ca-certificates \
    tzdata \
    make

RUN update-ca-certificates
# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum Makefile ./
RUN make deps

COPY . .
# Run go build and strip symbols / debug info
RUN make build

FROM scratch
ARG WORKDIR
ENV APP_ENV=production
WORKDIR $WORKDIR
# Copy CA certificates from the builder so that they are available to the application
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Uncomment this if you need timezone information in the image
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=builder $WORKDIR/bin/go-short-link .

EXPOSE 3000

ENTRYPOINT ["./go-short-link"]