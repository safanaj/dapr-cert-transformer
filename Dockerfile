FROM gcr.io/distroless/static

ADD dapr-cert-transformer /
CMD ["/dapr-cert-transformer"]
