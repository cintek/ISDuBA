#!/usr/bin/env bash

# This file is Free Software under the Apache-2.0 License
# without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
#
# SPDX-License-Identifier: Apache-2.0
#
# SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
# Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

set -e # to exit if a command in the script fails

sudo apt install -y unzip # needed to unzip the keycloak archive

version="26.0.0"

if [ ! -z "$1" ]; then
  version="$1"
fi

full="keycloak-$version.zip"

if [ -d /opt/keycloak ] && sudo /opt/keycloak/bin/kc.sh --version | grep -q -F "Keycloak $version"; then
  echo "A Keycloak installation already exists. Skipping installation."
else
  # Make sure no potentially broken zip file exists
  rm --force $full
  # download and extract keycloak
  wget https://github.com/keycloak/keycloak/releases/download/$version/$full

  echo "Extracting Keycloak..."
  unzip -q $full

  sudo mkdir -p /opt/

  sudo mv keycloak-$version /opt/keycloak
  rm --force $full
  echo "Successfully installed Keycloak at /opt/keycloak."
fi

# create a keycloak user and give them the rights over keycloak
if id "keycloak" >/dev/null 2>&1; then
  echo "User keycloak exists. Skipping creation."
else
  sudo adduser --disabled-password --system --group --gecos \"\" keycloak
  echo "Created user keycloak."
fi

sudo chown -R keycloak:keycloak /opt/keycloak
sudo chmod -R o-rwx /opt/keycloak/
