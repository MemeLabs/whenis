FROM golang as builder
ENV GO111MODULE=on
WORKDIR /code
ADD *.go go.mod go.sum /code/
RUN CGO_ENABLED=0 go build -o /whenis .

FROM alpine:latest 
WORKDIR /
COPY --from=builder /whenis /whenis
ENTRYPOINT ["/whenis"]
