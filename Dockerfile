# E2E image: build ngray, then run it for real against a clean Ubuntu base.
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN make build

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates openssl nginx libnginx-mod-stream iptables \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/ngray /usr/local/bin/ngray
COPY config.example.yml /root/config.yml
COPY snis /root/snis
WORKDIR /root
ENTRYPOINT ["/bin/bash"]
