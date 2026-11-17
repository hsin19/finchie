// The API's types, by name: nothing that runs, so a module any context loads, the service worker's
// included, can name them. Generated from apps/backend/api/openapi.yaml by `task web:generate`.

import type {
    components,
    paths,
} from "./schema";

type Schemas = components["schemas"];
export type Account = Schemas["Account"];
export type AccountDetail = Schemas["AccountDetail"];
export type AccountKind = Schemas["AccountKind"];
export type AccountLine = Schemas["AccountLine"];
export type Balance = Schemas["Balance"];
export type Expiry = Schemas["Expiry"];
export type BalanceGap = Schemas["BalanceGap"];
export type AccountSide = Schemas["AccountSide"];
export type CardTotal = Schemas["CardTotal"];
export type Debit = Schemas["Debit"];
export type NextLapse = Schemas["NextLapse"];
export type Notification = Schemas["Notification"];
export type NotificationKind = Schemas["NotificationKind"];
export type Shortfall = Schemas["Shortfall"];
export type Check = Schemas["Check"];
export type Document = Schemas["Document"];
export type DocumentPage = Schemas["DocumentPage"];
export type DocumentPreview = Schemas["DocumentPreview"];
export type DocumentFailure = Schemas["DocumentFailure"];
export type DocumentStatus = Schemas["DocumentStatus"];
export type InvestmentPoint = Schemas["InvestmentPoint"];
export type KindTotal = Schemas["KindTotal"];
export type LineCounterpart = Schemas["LineCounterpart"];
export type NetWorth = Schemas["NetWorth"];
export type NetWorthPoint = Schemas["NetWorthPoint"];
export type Position = Schemas["Position"];
export type PreviewFile = Schemas["PreviewFile"];
export type Price = Schemas["Price"];
export type PriceSource = Schemas["PriceSource"];
export type StateCount = Schemas["StateCount"];
export type Settings = Schemas["Settings"];
export type Statement = Schemas["Statement"];
export type Trade = Schemas["Trade"];
export type Transaction = Schemas["Transaction"];
export type TransactionFamily = Schemas["TransactionFamily"];
export type TransactionKind = Schemas["TransactionKind"];
export type TransactionLink = Schemas["TransactionLink"];
export type TransactionSearch = Schemas["TransactionSearch"];
export type TransactionSort = Schemas["TransactionSort"];
export type UnitFlow = Schemas["UnitFlow"];
export type TransactionQuery = NonNullable<paths["/api/transactions"]["get"]["parameters"]["query"]>;
