FROM golang as builder
ENV GO111MODULE=on
WORKDIR /code
ADD *.go go.mod go.sum /code/
RUN go build -o /whenis .

FROM alpine:latest 
WORKDIR /
COPY --from=builder /whenis /usr/bin/whenis
ENTRYPOINT ["/usr/bin/radio"]