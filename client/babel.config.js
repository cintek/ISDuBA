// This file is Free Software under the Apache-2.0 License
// without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
//
// SPDX-License-Identifier: Apache-2.0
//
// SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
// Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

const plugins = [];

// Instrument for code coverage in development mode
if (process.env.NODE_ENV === "development") {
  // eslint-disable-next-line no-console
  console.log("Detected development environment. Instrumenting code for coverage.");
  plugins.push("istanbul");
}

module.exports = {
  plugins
};
