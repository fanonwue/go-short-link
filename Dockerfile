ARG WORKDIR=/opt/go-short-link

FROM golang:1.21-alpine as builder
ARG WORKDIR
WORKDIR $WORKDIR
COPY . .
RUN go build -o go-short-link

FROM alpine
ARG WORKDIR
ENV APP_ENV production
WORKDIR $WORKDIR
COPY --from=builder $WORKDIR/go-short-link .
COPY resources $WORKDIR/resources

EXPOSE 3000

ENTRYPOINT ["./go-short-link"]