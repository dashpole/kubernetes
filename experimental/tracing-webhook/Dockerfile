FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY tracing-webhook /app/tracing-webhook
EXPOSE 8443
ENTRYPOINT ["/app/tracing-webhook"]
