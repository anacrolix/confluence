FROM golang

WORKDIR /go/src/confluence/

COPY ./go.mod .
COPY ./go.sum .
RUN go mod download -x

COPY . .
RUN go install -v

ENTRYPOINT [ "confluence" ]
CMD [ "-h" ]
