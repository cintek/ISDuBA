/**
 * This file is Free Software under the Apache-2.0 License
 * without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
 *
 * SPDX-License-Identifier: Apache-2.0
 *
 * SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
 * Software-Engineering: 2024 Intevation GmbH <https://intevation.de>
 */

import { appStore } from "./store";
import { push } from "svelte-spa-router";
import type { User } from "oidc-client-ts";
import { ERRORTYPES } from "./Errors/error";

export const request = async (
  path: string,
  requestMethod: string,
  formData?: FormData
): Promise<any> => {
  try {
    const token = await getAccessToken();
    const response = await fetch(path, {
      headers: {
        Authorization: `Bearer ${token}`
      },
      method: requestMethod,
      body: formData
    });
    const contentType = response.headers.get("content-type");
    const isJson = contentType?.includes("application/json");
    if (response.ok) {
      if (contentType && isJson) {
        const json = await response.json();
        return { content: json, ok: true };
      } else {
        const text = await response.text();
        return { content: text, ok: true };
      }
    } else {
      if (response.status == 400) {
        return { error: `${response.status}`, errorType: ERRORTYPES.CLIENTERROR };
      }
      if (response.status == 401) {
        appStore.setSessionExpired(true);
        appStore.setSessionExpiredMessage("User unauthorized");
        await push("/login");
      }
      if (response.status == 402) {
        return { error: `${response.status}`, errorType: ERRORTYPES.AUTHORIZATIONERROR };
      }
      if (response.status == 500) {
        return { error: `${response.status}`, errorType: ERRORTYPES.SERVERERROR };
      }
      if (contentType && isJson) {
        const json = await response.json();
        return {
          error: `${json.error ?? json.message}`,
          ok: false,
          errorType: ERRORTYPES.PAYLOADERROR
        };
      } else {
        return {
          error: `${response.status}: ${response.statusText}`,
          ok: false,
          errorType: ERRORTYPES.GENERALERROR
        };
      }
    }
  } catch (error: any) {
    return {
      error: `${error.name}: ${error.message}`,
      ok: false,
      errorType: ERRORTYPES.GENERALERROR
    };
  }
};

const getAccessToken = async () => {
  const userManager = appStore.getUserManager();
  return userManager.getUser().then(async (user: User) => {
    if (user) {
      return user.access_token;
    } else {
      await push("/login");
    }
  });
};
