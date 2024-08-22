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
import type { HttpResponse } from "./types";
import { jwtDecode } from "jwt-decode";

export const request = async (
  path: string,
  requestMethod: string,
  formData?: FormData | string,
  abortController?: AbortController
): Promise<HttpResponse> => {
  try {
    const token = await getAccessToken();
    let response;
    if (abortController) {
      response = await fetch(path, {
        headers: {
          Authorization: `Bearer ${token}`
        },
        method: requestMethod,
        body: formData,
        signal: abortController.signal
      });
    } else {
      response = await fetch(path, {
        headers: {
          Authorization: `Bearer ${token}`
        },
        method: requestMethod,
        body: formData
      });
    }
    const contentType = response.headers.get("content-type");
    const isJson = contentType?.includes("application/json");
    let json;
    if (contentType && isJson) {
      try {
        json = await response.json();
      } catch (_) {
        return {
          error: "783", // Used by Shopify to indicate that the request includes a JSON syntax error. See https://shopify.dev/docs/api/usage/response-codes
          content: `${json.error}`,
          ok: false
        };
      }
    }
    let content;
    if (contentType && isJson) {
      content = json;
    } else {
      content = await response.text();
    }
    if (response.ok) {
      return { content: content, ok: true };
    }
    if (response.status == 401) {
      appStore.setSessionExpired(true);
      appStore.setSessionExpiredMessage("User unauthorized");
      await push("/login");
    }
    if (contentType && isJson) {
      return { error: `${response.status}`, content: json.error, ok: false };
    }
    switch (response.status) {
      case 400:
      case 403:
      case 500:
        return { error: `${response.status}`, content: content, ok: false };
      default:
      // do nothing and return later
    }
    return { error: `${response.status}`, content: content, ok: false };
  } catch (error: any) {
    if (/fetch/.test(error)) {
      return {
        error: "600",
        content: error,
        ok: false
      };
    }
    return {
      error: `${error.name}: ${error.message}`,
      ok: false
    };
  }
};

const getAccessToken = async () => {
  const userManager = appStore.getUserManager();
  return userManager.getUser().then(async (user: User) => {
    if (user) {
      appStore.setTokenParsed(jwtDecode(user.access_token));
      return user.access_token;
    } else {
      await push("/login");
    }
  });
};
