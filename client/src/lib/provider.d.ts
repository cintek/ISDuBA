/**
 * This file is Free Software under the Apache-2.0 License
 * without warranty, see README.md and LICENSES/Apache-2.0.txt for details.
 *
 * SPDX-License-Identifier: Apache-2.0
 *
 * SPDX-FileCopyrightText: 2024 German Federal Office for Information Security (BSI) <https://www.bsi.bund.de>
 * Software-Engineering: 2024 Intevation GmbH <https://intevation.de>
 */

/**
 * This file was automatically generated by json-schema-to-typescript.
 * DO NOT MODIFY IT BY HAND. Instead, modify the source JSONSchema file,
 * and run json-schema-to-typescript to regenerate this file.
 */

/**
 * Contains the URL for this document.
 */
export type CanonicalURL = string;
/**
 * Contains a list of used distribution mechanisms.
 *
 * @minItems 1
 */
export type ListOfDistribution = [Distribution, ...Distribution[]];
/**
 * Contains the base url for the directory distribution.
 */
export type DirectoryURL = string;
/**
 * Contains a list of URLs which contain ROLIE category documents.
 *
 * @minItems 1
 */
export type ListOfROLIECategoryDocumentURLs = [
  ROLIECategoryDocumentURL,
  ...ROLIECategoryDocumentURL[]
];
/**
 * Contains a URL of a ROLIE category document.
 */
export type ROLIECategoryDocumentURL = string;
/**
 * Contains a list of information about ROLIE feeds.
 *
 * @minItems 1
 */
export type ListOfROLIEFeeds = [ROLIEFeed, ...ROLIEFeed[]];
/**
 * Contains a summary of the feed.
 */
export type SummaryOfTheFeed = string;
/**
 * Provides the TLP label for the feed.
 */
export type TLPLabel = "UNLABELED" | "WHITE" | "GREEN" | "AMBER" | "RED";
/**
 * Contains the URL of the feed.
 */
export type URLOfTheFeed = string;
/**
 * Contains a list of URLs which contain ROLIE service documents.
 *
 * @minItems 1
 */
export type ListOfROLIEServiceDocumentURLs = [
  ROLIEServiceDocumentURL,
  ...ROLIEServiceDocumentURL[]
];
/**
 * Contains a URL of a ROLIE service document.
 */
export type ROLIEServiceDocumentURL = string;
/**
 * Holds the date and time when the document was last updated.
 */
export type LastUpdated = string;
/**
 * Decides whether this file should be linked in the list of a CSAF aggregator.
 */
export type ListOnCSAFAggregators = boolean;
/**
 * Gives the version of the CSAF provider metadata specification which the document was generated for.
 */
export type CSAFProviderMetadataVersion = "2.0";
/**
 * Decides whether the CSAF documents can be mirrored and provided by a CSAF aggregator.
 */
export type MirrorOnCSAFAggregators = boolean;
/**
 * Contains the fingerprint of the OpenPGP key.
 */
export type FingerprintOfTheKey = string;
/**
 * Contains the URL where the key can be retrieved.
 */
export type URLOfTheKey = string;
/**
 * Contains a list of OpenPGP keys used to sign CSAF documents.
 */
export type ListOfPublicOpenPGPKeys = PGPKeys[];
/**
 * Provides information about the category of publisher releasing the document.
 */
export type CategoryOfPublisher =
  | "coordinator"
  | "discoverer"
  | "other"
  | "translator"
  | "user"
  | "vendor";
/**
 * Information on how to contact the publisher, possibly including details such as web sites, email addresses, phone numbers, and postal mail addresses.
 */
export type ContactDetails = string;
/**
 * Provides information about the authority of the issuing party to release the document, in particular, the party's constituency and responsibilities or other obligations.
 */
export type IssuingAuthority = string;
/**
 * Contains the name of the issuing party.
 */
export type NameOfPublisher = string;
/**
 * Contains a URL which is under control of the issuing party and can be used as a globally unique identifier for that issuing party.
 */
export type NamespaceOfPublisher = string;
/**
 * Contains the role of the issuing party according to section 7 in the CSAF standard.
 */
export type RoleOfTheIssuingParty = "csaf_publisher" | "csaf_provider" | "csaf_trusted_provider";

/**
 * Representation of metadata information of a CSAF provider as a JSON document.
 */
export interface CSAFProviderMetadata {
  canonical_url: CanonicalURL;
  distributions?: ListOfDistribution;
  last_updated: LastUpdated;
  list_on_CSAF_aggregators: ListOnCSAFAggregators;
  metadata_version: CSAFProviderMetadataVersion;
  mirror_on_CSAF_aggregators: MirrorOnCSAFAggregators;
  public_openpgp_keys?: ListOfPublicOpenPGPKeys;
  publisher: Publisher;
  role: RoleOfTheIssuingParty;
  [k: string]: unknown;
}
/**
 * Contains the information of a used distribution mechanism.
 */
export interface Distribution {
  directory_url?: DirectoryURL;
  rolie?: ROLIE;
  [k: string]: unknown;
}
/**
 * Contains all information for ROLIE distribution.
 */
export interface ROLIE {
  categories?: ListOfROLIECategoryDocumentURLs;
  feeds: ListOfROLIEFeeds;
  services?: ListOfROLIEServiceDocumentURLs;
  [k: string]: unknown;
}
/**
 * Contains information about the ROLIE feed.
 */
export interface ROLIEFeed {
  summary?: SummaryOfTheFeed;
  tlp_label: TLPLabel;
  url: URLOfTheFeed;
  [k: string]: unknown;
}
/**
 * Contains all information about an OpenPGP key used to sign CSAF documents.
 */
export interface PGPKeys {
  fingerprint?: FingerprintOfTheKey;
  url: URLOfTheKey;
  [k: string]: unknown;
}
/**
 * Provides information about the publisher of the CSAF documents in this repository.
 */
export interface Publisher {
  category: CategoryOfPublisher;
  contact_details?: ContactDetails;
  issuing_authority?: IssuingAuthority;
  name: NameOfPublisher;
  namespace: NamespaceOfPublisher;
  [k: string]: unknown;
}
