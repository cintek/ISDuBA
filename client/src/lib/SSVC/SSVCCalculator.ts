// This file is Free Software under the MIT License
// without warranty, see README.md and LICENSES/MIT.txt for details.
//
// SPDX-License-Identifier: Apache-2.0
//
// SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
//  Software-Engineering: 2024 Intevation GmbH <https://intevation.de>

export function loadDecisionTreeFromFile() {
  return new Promise((resolve) => {
    fetch("CISA-Coordinator.json").then((response) => {
      response.json().then((json) => {
        const addedPoints: string[] = [];
        const decisionPoints = json.decision_points;
        const decisionsTable = json.decisions_table;
        let mainDecisions = [];
        for (let i = decisionPoints.length - 1; i >= 0; i--) {
          const decision = decisionPoints[i];
          if (!addedPoints.includes(decision.label)) {
            mainDecisions.push(decision);
            if (decision.decision_type === "complex") {
              for (const child of decision.children) {
                addedPoints.push(child.label);
              }
            } else {
              addedPoints.push(decision.label);
            }
          }
        }
        mainDecisions = mainDecisions.reverse();
        const steps = mainDecisions.map((element) => element.label);
        resolve({
          decisionPoints: decisionPoints,
          decisionsTable: decisionsTable,
          mainDecisions: mainDecisions,
          steps: steps
        });
      });
    });
  });
}

export function getOptionWithKey(decision: any, key: string): any {
  return decision.options.find((element: any) => element.key === key);
}

export function createIsoTimeStringForSSVC() {
  const iso = new Date().toISOString();
  return `${iso.split(".")[0]}Z`;
}

export async function convertVectorToLabel(vector: string, mainDecisions?: any[]): any {
  if (!mainDecisions) {
    ({ mainDecisions } = await loadDecisionTreeFromFile());
  }
  const keyPairs = vector.split("/").slice(1, -2);
  if (mainDecisions && mainDecisions.length === keyPairs.length) {
    const keyOfSelectedOption = keyPairs[keyPairs.length - 1].split(":")[1];
    const selectedOption = getOptionWithKey(
      mainDecisions[mainDecisions.length - 1],
      keyOfSelectedOption
    );
    return {
      label: selectedOption.label,
      color: selectedOption.color
    };
  }
}