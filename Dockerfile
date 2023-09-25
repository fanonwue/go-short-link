ARG WORKDIR=/opt/app

FROM golang:1.21-alpine as builder
ARG WORKDIR
WORKDIR $WORKDIR

# make is needed for the Makefile
RUN apk update && apk add --no-cache make

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
# Run go build and strip symbols / debug info
RUN make build TARGET=prod

FROM alpine
ARG WORKDIR
ENV APP_ENV production
WORKDIR $WORKDIR
COPY --from=builder $WORKDIR/bin/go-short-link .
COPY resources $WORKDIR/resources

EXPOSE 3000

ENTRYPOINT ["./go-short-link"]