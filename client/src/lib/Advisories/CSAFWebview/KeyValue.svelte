<!--
 This file is Free Software under the Apache-2.0 License
 without warranty, see README.md and LICENSES/Apache-2.0.txt for details.

 SPDX-License-Identifier: Apache-2.0

 SPDX-FileCopyrightText: 2023 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
 Software-Engineering: 2023 Intevation GmbH <https://intevation.de>
-->

<script lang="ts">
  /* eslint-disable svelte/no-at-html-tags */
  import { marked } from "marked";
  import DOMPurify from "dompurify";
  import { Table, TableBody, TableBodyCell, TableBodyRow } from "flowbite-svelte";
  export let keys: Array<string>;
  export let values: any;
  marked.use({ gfm: true });
  const cellStyle = "px-6 py-0";

  const getMarkdown = (index: number) => {
    return marked.parse(
      // eslint-disable-next-line no-misleading-character-class
      values[index].replace(/^[\u200B\u200C\u200D\u200E\u200F\uFEFF]/, "")
    ) as string;
  };
</script>

<div class="ml-2 w-fit">
  <Table noborder>
    <TableBody>
      {#each keys as key, index}
        {#if key === "text" || key === "Text"}
          <TableBodyRow color="custom">
            <TableBodyCell tdClass={cellStyle}>{key}</TableBodyCell>
            <TableBodyCell tdClass={cellStyle}>
              <div class="markdown-text">
                <div class="display-markdown max-w-2/3">
                  {@html DOMPurify.sanitize(getMarkdown(index))}
                </div>
              </div>
            </TableBodyCell>
          </TableBodyRow>
        {:else}
          <TableBodyRow color="custom"
            ><TableBodyCell tdClass={cellStyle}>{key}</TableBodyCell>
            <TableBodyCell tdClass={cellStyle}>
              {#if values[index].startsWith("https://")}
                <a class="underline" href={values[index]}>
                  <i class="bx bx-link"></i>{values[index]}
                </a>
              {:else}
                {values[index]}
              {/if}
            </TableBodyCell>
          </TableBodyRow>
        {/if}
      {/each}
    </TableBody>
  </Table>
</div>

<style>
  .markdown-text {
    padding: 0.5rem;
    border: 1px solid lightgray;
  }
</style>
