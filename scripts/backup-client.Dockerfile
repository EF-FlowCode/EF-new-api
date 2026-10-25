FROM mysql:8.4.8
COPY backup-client.repo /etc/yum.repos.d/mysql-full-client.repo
RUN microdnf remove -y mysql-community-server-minimal \
    && microdnf install -y mysql-community-client-8.4.8 python3.11 \
    && microdnf clean all \
    && mysqlbinlog --version
ENTRYPOINT []
CMD ["mysqlbinlog", "--version"]
