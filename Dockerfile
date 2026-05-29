FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/logs-dashboard .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/logs-dashboard /logs-dashboard
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/logs-dashboard", "-mode=health"]
ENTRYPOINT ["/logs-dashboard"]
CMD ["-mode=serve"]
