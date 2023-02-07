FROM --platform=${BUILDPLATFORM} golang:1.19 as builder

WORKDIR /workspace

COPY go.mod go.mod

COPY go.sum go.sum

RUN go mod download

COPY cmd cmd

COPY pkg pkg

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o main cmd/controller/main.go


FROM katanomi/distroless-static:nonroot

COPY --from=builder /workspace/main /bin/main

ENV ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH=go1.19

ENTRYPOINT ["/bin/main"]
