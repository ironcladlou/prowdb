FROM golang:1.14 AS builder

WORKDIR /operator

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -gcflags=all="-N -l" -o ez-thanos-operator .

FROM registry.access.redhat.com/ubi8/ubi:latest
COPY --from=builder /operator/ez-thanos-operator /usr/bin/

CMD /usr/bin/ez-thanos-operator
