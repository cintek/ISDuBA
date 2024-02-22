#!/usr/bin/env bash
set -e

apt install -y unzip

wget https://github.com/keycloak/keycloak/releases/download/23.0.5/keycloak-23.0.5.zip

unzip keycloak-23.0.5.zip

mkdir /opt/

mv keycloak-23.0.5 /opt/keycloak

useradd keycloak
chown -R keycloak: /opt/keycloak

echo "# Basic settings for running in production. Change accordingly before deploying the server.

# Database

# The database vendor.
db=postgres

# The username of the database user.
db-username=keycloak

# The password of the database user.
db-password=keycloak

# The full database JDBC URL. 
# If not provided, a default URL is set based on the selected database vendor.
db-url=jdbc:postgresql://localhost/keycloak

# Observability

# If the server should expose healthcheck endpoints.
#health-enabled=true

# If the server should expose metrics endpoints.
#metrics-enabled=true

# HTTP

# The file path to a server certificate or certificate chain in PEM format.
#https-certificate-file=${kc.home.dir}conf/server.crt.pem

# The file path to a private key in PEM format.
#https-certificate-key-file=${kc.home.dir}conf/server.key.pem

# The proxy address forwarding mode if the server is behind a reverse proxy.
#proxy=reencrypt

# Do not attach route to cookies and rely on the session affinity capabilities from reverse proxy
#spi-sticky-session-encoder-infinispan-should-attach-route=false

# Hostname for the Keycloak server.
#hostname=isduba" > /opt/keycloak/conf/keycloak.conf

bin/kc.sh start-dev &
