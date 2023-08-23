FROM scratch
COPY ./core-dump-uploader   /core-dump-uploader
COPY ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/core-dump-uploader"]