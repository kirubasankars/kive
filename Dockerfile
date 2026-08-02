# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.

FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /kive
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o kive && chmod +x kive

FROM alpine:3.21
COPY --from=builder /kive/kive /usr/local/bin/kive
ENTRYPOINT ["kive"]
