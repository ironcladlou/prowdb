FROM golang:1.14 AS builder

WORKDIR /operator

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -gcflags=all="-N -l" -o dowser .

FROM registry.access.redhat.com/ubi8/ubi:latest
COPY --from=builder /operator/dowser /usr/bin/

CMD /usr/bin/dowser
