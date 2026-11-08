import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.type import date_pb2 as _date_pb2
from google.type import decimal_pb2 as _decimal_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TransactionKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRANSACTION_KIND_UNSPECIFIED: _ClassVar[TransactionKind]
    TRANSACTION_KIND_PURCHASE: _ClassVar[TransactionKind]
    TRANSACTION_KIND_FOREIGN_TRANSACTION_FEE: _ClassVar[TransactionKind]
    TRANSACTION_KIND_ANNUAL_FEE: _ClassVar[TransactionKind]
    TRANSACTION_KIND_REFUND: _ClassVar[TransactionKind]
    TRANSACTION_KIND_REBATE: _ClassVar[TransactionKind]
    TRANSACTION_KIND_PAYMENT: _ClassVar[TransactionKind]

class DepositLineKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    DEPOSIT_LINE_KIND_UNSPECIFIED: _ClassVar[DepositLineKind]
    DEPOSIT_LINE_KIND_OTHER: _ClassVar[DepositLineKind]
    DEPOSIT_LINE_KIND_INTEREST: _ClassVar[DepositLineKind]
    DEPOSIT_LINE_KIND_FEE: _ClassVar[DepositLineKind]

class TradeSide(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRADE_SIDE_UNSPECIFIED: _ClassVar[TradeSide]
    TRADE_SIDE_BUY: _ClassVar[TradeSide]
    TRADE_SIDE_SELL: _ClassVar[TradeSide]
TRANSACTION_KIND_UNSPECIFIED: TransactionKind
TRANSACTION_KIND_PURCHASE: TransactionKind
TRANSACTION_KIND_FOREIGN_TRANSACTION_FEE: TransactionKind
TRANSACTION_KIND_ANNUAL_FEE: TransactionKind
TRANSACTION_KIND_REFUND: TransactionKind
TRANSACTION_KIND_REBATE: TransactionKind
TRANSACTION_KIND_PAYMENT: TransactionKind
DEPOSIT_LINE_KIND_UNSPECIFIED: DepositLineKind
DEPOSIT_LINE_KIND_OTHER: DepositLineKind
DEPOSIT_LINE_KIND_INTEREST: DepositLineKind
DEPOSIT_LINE_KIND_FEE: DepositLineKind
TRADE_SIDE_UNSPECIFIED: TradeSide
TRADE_SIDE_BUY: TradeSide
TRADE_SIDE_SELL: TradeSide

class ParseDocumentRequest(_message.Message):
    __slots__ = ("document_id", "filename", "content_type", "content")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    filename: str
    content_type: str
    content: bytes
    def __init__(self, document_id: _Optional[str] = ..., filename: _Optional[str] = ..., content_type: _Optional[str] = ..., content: _Optional[bytes] = ...) -> None: ...

class OpenDocumentRequest(_message.Message):
    __slots__ = ("document_id", "filename", "content_type", "content")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    filename: str
    content_type: str
    content: bytes
    def __init__(self, document_id: _Optional[str] = ..., filename: _Optional[str] = ..., content_type: _Optional[str] = ..., content: _Optional[bytes] = ...) -> None: ...

class OpenDocumentResponse(_message.Message):
    __slots__ = ("email", "files")
    EMAIL_FIELD_NUMBER: _ClassVar[int]
    FILES_FIELD_NUMBER: _ClassVar[int]
    email: Email
    files: _containers.RepeatedCompositeFieldContainer[File]
    def __init__(self, email: _Optional[_Union[Email, _Mapping]] = ..., files: _Optional[_Iterable[_Union[File, _Mapping]]] = ...) -> None: ...

class Email(_message.Message):
    __slots__ = ("sender", "recipient", "subject", "date", "html", "text")
    SENDER_FIELD_NUMBER: _ClassVar[int]
    RECIPIENT_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    DATE_FIELD_NUMBER: _ClassVar[int]
    HTML_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    sender: str
    recipient: str
    subject: str
    date: _timestamp_pb2.Timestamp
    html: str
    text: str
    def __init__(self, sender: _Optional[str] = ..., recipient: _Optional[str] = ..., subject: _Optional[str] = ..., date: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., html: _Optional[str] = ..., text: _Optional[str] = ...) -> None: ...

class File(_message.Message):
    __slots__ = ("filename", "content_type", "content", "locked", "primary")
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    LOCKED_FIELD_NUMBER: _ClassVar[int]
    PRIMARY_FIELD_NUMBER: _ClassVar[int]
    filename: str
    content_type: str
    content: bytes
    locked: bool
    primary: bool
    def __init__(self, filename: _Optional[str] = ..., content_type: _Optional[str] = ..., content: _Optional[bytes] = ..., locked: _Optional[bool] = ..., primary: _Optional[bool] = ...) -> None: ...

class ParseDocumentResponse(_message.Message):
    __slots__ = ("parser", "parser_version", "statement", "warnings", "deposit_statement", "securities_statement")
    PARSER_FIELD_NUMBER: _ClassVar[int]
    PARSER_VERSION_FIELD_NUMBER: _ClassVar[int]
    STATEMENT_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    DEPOSIT_STATEMENT_FIELD_NUMBER: _ClassVar[int]
    SECURITIES_STATEMENT_FIELD_NUMBER: _ClassVar[int]
    parser: str
    parser_version: str
    statement: Statement
    warnings: _containers.RepeatedScalarFieldContainer[str]
    deposit_statement: DepositStatement
    securities_statement: SecuritiesStatement
    def __init__(self, parser: _Optional[str] = ..., parser_version: _Optional[str] = ..., statement: _Optional[_Union[Statement, _Mapping]] = ..., warnings: _Optional[_Iterable[str]] = ..., deposit_statement: _Optional[_Union[DepositStatement, _Mapping]] = ..., securities_statement: _Optional[_Union[SecuritiesStatement, _Mapping]] = ...) -> None: ...

class Statement(_message.Message):
    __slots__ = ("institution", "closing_date", "due_date", "currency", "total_due", "transactions", "reconciliation")
    INSTITUTION_FIELD_NUMBER: _ClassVar[int]
    CLOSING_DATE_FIELD_NUMBER: _ClassVar[int]
    DUE_DATE_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    TOTAL_DUE_FIELD_NUMBER: _ClassVar[int]
    TRANSACTIONS_FIELD_NUMBER: _ClassVar[int]
    RECONCILIATION_FIELD_NUMBER: _ClassVar[int]
    institution: str
    closing_date: _date_pb2.Date
    due_date: _date_pb2.Date
    currency: str
    total_due: _decimal_pb2.Decimal
    transactions: _containers.RepeatedCompositeFieldContainer[Transaction]
    reconciliation: Reconciliation
    def __init__(self, institution: _Optional[str] = ..., closing_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., due_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., currency: _Optional[str] = ..., total_due: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., transactions: _Optional[_Iterable[_Union[Transaction, _Mapping]]] = ..., reconciliation: _Optional[_Union[Reconciliation, _Mapping]] = ...) -> None: ...

class Transaction(_message.Message):
    __slots__ = ("transaction_date", "posting_date", "description", "kind", "amount", "original_amount", "original_currency", "country", "card_last4")
    TRANSACTION_DATE_FIELD_NUMBER: _ClassVar[int]
    POSTING_DATE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ORIGINAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ORIGINAL_CURRENCY_FIELD_NUMBER: _ClassVar[int]
    COUNTRY_FIELD_NUMBER: _ClassVar[int]
    CARD_LAST4_FIELD_NUMBER: _ClassVar[int]
    transaction_date: _date_pb2.Date
    posting_date: _date_pb2.Date
    description: str
    kind: TransactionKind
    amount: _decimal_pb2.Decimal
    original_amount: _decimal_pb2.Decimal
    original_currency: str
    country: str
    card_last4: str
    def __init__(self, transaction_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., posting_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., description: _Optional[str] = ..., kind: _Optional[_Union[TransactionKind, str]] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., original_amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., original_currency: _Optional[str] = ..., country: _Optional[str] = ..., card_last4: _Optional[str] = ...) -> None: ...

class Reconciliation(_message.Message):
    __slots__ = ("balanced", "computed", "stated", "stated_label")
    BALANCED_FIELD_NUMBER: _ClassVar[int]
    COMPUTED_FIELD_NUMBER: _ClassVar[int]
    STATED_FIELD_NUMBER: _ClassVar[int]
    STATED_LABEL_FIELD_NUMBER: _ClassVar[int]
    balanced: bool
    computed: _decimal_pb2.Decimal
    stated: _decimal_pb2.Decimal
    stated_label: str
    def __init__(self, balanced: _Optional[bool] = ..., computed: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., stated: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., stated_label: _Optional[str] = ...) -> None: ...

class DepositStatement(_message.Message):
    __slots__ = ("institution", "period_start", "period_end", "accounts")
    INSTITUTION_FIELD_NUMBER: _ClassVar[int]
    PERIOD_START_FIELD_NUMBER: _ClassVar[int]
    PERIOD_END_FIELD_NUMBER: _ClassVar[int]
    ACCOUNTS_FIELD_NUMBER: _ClassVar[int]
    institution: str
    period_start: _date_pb2.Date
    period_end: _date_pb2.Date
    accounts: _containers.RepeatedCompositeFieldContainer[DepositAccount]
    def __init__(self, institution: _Optional[str] = ..., period_start: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., period_end: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., accounts: _Optional[_Iterable[_Union[DepositAccount, _Mapping]]] = ...) -> None: ...

class DepositAccount(_message.Message):
    __slots__ = ("number", "name", "currency", "opening_balance", "closing_balance", "lines", "reconciliation")
    NUMBER_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    OPENING_BALANCE_FIELD_NUMBER: _ClassVar[int]
    CLOSING_BALANCE_FIELD_NUMBER: _ClassVar[int]
    LINES_FIELD_NUMBER: _ClassVar[int]
    RECONCILIATION_FIELD_NUMBER: _ClassVar[int]
    number: str
    name: str
    currency: str
    opening_balance: _decimal_pb2.Decimal
    closing_balance: _decimal_pb2.Decimal
    lines: _containers.RepeatedCompositeFieldContainer[DepositLine]
    reconciliation: Reconciliation
    def __init__(self, number: _Optional[str] = ..., name: _Optional[str] = ..., currency: _Optional[str] = ..., opening_balance: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., closing_balance: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., lines: _Optional[_Iterable[_Union[DepositLine, _Mapping]]] = ..., reconciliation: _Optional[_Union[Reconciliation, _Mapping]] = ...) -> None: ...

class DepositLine(_message.Message):
    __slots__ = ("date", "description", "memo", "counterparty", "amount", "balance", "kind")
    DATE_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    MEMO_FIELD_NUMBER: _ClassVar[int]
    COUNTERPARTY_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    BALANCE_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    date: _date_pb2.Date
    description: str
    memo: str
    counterparty: str
    amount: _decimal_pb2.Decimal
    balance: _decimal_pb2.Decimal
    kind: DepositLineKind
    def __init__(self, date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., description: _Optional[str] = ..., memo: _Optional[str] = ..., counterparty: _Optional[str] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., balance: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., kind: _Optional[_Union[DepositLineKind, str]] = ...) -> None: ...

class SecuritiesStatement(_message.Message):
    __slots__ = ("institution", "account_number", "period_start", "period_end", "holdings_date", "holdings", "trades", "reconciliation")
    INSTITUTION_FIELD_NUMBER: _ClassVar[int]
    ACCOUNT_NUMBER_FIELD_NUMBER: _ClassVar[int]
    PERIOD_START_FIELD_NUMBER: _ClassVar[int]
    PERIOD_END_FIELD_NUMBER: _ClassVar[int]
    HOLDINGS_DATE_FIELD_NUMBER: _ClassVar[int]
    HOLDINGS_FIELD_NUMBER: _ClassVar[int]
    TRADES_FIELD_NUMBER: _ClassVar[int]
    RECONCILIATION_FIELD_NUMBER: _ClassVar[int]
    institution: str
    account_number: str
    period_start: _date_pb2.Date
    period_end: _date_pb2.Date
    holdings_date: _date_pb2.Date
    holdings: _containers.RepeatedCompositeFieldContainer[SecuritiesHolding]
    trades: _containers.RepeatedCompositeFieldContainer[SecuritiesTrade]
    reconciliation: Reconciliation
    def __init__(self, institution: _Optional[str] = ..., account_number: _Optional[str] = ..., period_start: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., period_end: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., holdings_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., holdings: _Optional[_Iterable[_Union[SecuritiesHolding, _Mapping]]] = ..., trades: _Optional[_Iterable[_Union[SecuritiesTrade, _Mapping]]] = ..., reconciliation: _Optional[_Union[Reconciliation, _Mapping]] = ...) -> None: ...

class SecuritiesHolding(_message.Message):
    __slots__ = ("name", "shares")
    NAME_FIELD_NUMBER: _ClassVar[int]
    SHARES_FIELD_NUMBER: _ClassVar[int]
    name: str
    shares: _decimal_pb2.Decimal
    def __init__(self, name: _Optional[str] = ..., shares: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...

class SecuritiesTrade(_message.Message):
    __slots__ = ("date", "name", "side", "shares", "price", "category", "amount", "fee", "tax", "net", "prepaid")
    DATE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SIDE_FIELD_NUMBER: _ClassVar[int]
    SHARES_FIELD_NUMBER: _ClassVar[int]
    PRICE_FIELD_NUMBER: _ClassVar[int]
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    FEE_FIELD_NUMBER: _ClassVar[int]
    TAX_FIELD_NUMBER: _ClassVar[int]
    NET_FIELD_NUMBER: _ClassVar[int]
    PREPAID_FIELD_NUMBER: _ClassVar[int]
    date: _date_pb2.Date
    name: str
    side: TradeSide
    shares: _decimal_pb2.Decimal
    price: _decimal_pb2.Decimal
    category: str
    amount: _decimal_pb2.Decimal
    fee: _decimal_pb2.Decimal
    tax: _decimal_pb2.Decimal
    net: _decimal_pb2.Decimal
    prepaid: _decimal_pb2.Decimal
    def __init__(self, date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., name: _Optional[str] = ..., side: _Optional[_Union[TradeSide, str]] = ..., shares: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., price: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., category: _Optional[str] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., fee: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., tax: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., net: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., prepaid: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...
