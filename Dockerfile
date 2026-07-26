FROM golang:1.26.5-alpine3.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/recon ./cmd/recon
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verify ./cmd/verify

FROM scratch AS runtime

COPY --from=build /out/recon /recon
COPY --from=build /out/verify /verify
COPY --from=build /src/testdata/golden /demo-data
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/recon"]
CMD ["serve", "-listen=:8080"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/recon", "healthcheck"]
