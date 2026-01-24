FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /fencemaster ./cmd/webhook

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /fencemaster /fencemaster

USER nonroot:nonroot

ENTRYPOINT ["/fencemaster"]
