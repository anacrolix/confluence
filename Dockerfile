FROM golang:bullseye AS builder

WORKDIR /go/src/confluence/

COPY ./go.mod .
COPY ./go.sum .
RUN go mod download -x

COPY . .
RUN go build -v -o bin


FROM debian:bullseye

COPY --from=builder /go/src/confluence/bin /usr/local/bin/confluence

ENTRYPOINT [ "confluence" ]
CMD [ "-h" ]
