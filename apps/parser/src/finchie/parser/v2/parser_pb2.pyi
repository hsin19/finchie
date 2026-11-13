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

class AccountKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ACCOUNT_KIND_UNSPECIFIED: _ClassVar[AccountKind]
    ACCOUNT_KIND_CREDIT_CARD: _ClassVar[AccountKind]
    ACCOUNT_KIND_DEPOSIT: _ClassVar[AccountKind]
    ACCOUNT_KIND_SECURITIES: _ClassVar[AccountKind]
    ACCOUNT_KIND_POINTS: _ClassVar[AccountKind]
    ACCOUNT_KIND_LOAN: _ClassVar[AccountKind]

class LineKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    LINE_KIND_UNSPECIFIED: _ClassVar[LineKind]
    LINE_KIND_PURCHASE: _ClassVar[LineKind]
    LINE_KIND_FOREIGN_TRANSACTION_FEE: _ClassVar[LineKind]
    LINE_KIND_ANNUAL_FEE: _ClassVar[LineKind]
    LINE_KIND_REFUND: _ClassVar[LineKind]
    LINE_KIND_REBATE: _ClassVar[LineKind]
    LINE_KIND_PAYMENT: _ClassVar[LineKind]
    LINE_KIND_INTEREST: _ClassVar[LineKind]
    LINE_KIND_FEE: _ClassVar[LineKind]
    LINE_KIND_OTHER: _ClassVar[LineKind]
    LINE_KIND_BUY: _ClassVar[LineKind]
    LINE_KIND_SELL: _ClassVar[LineKind]
    LINE_KIND_EARN: _ClassVar[LineKind]
    LINE_KIND_ADJUST: _ClassVar[LineKind]
    LINE_KIND_REDEEM: _ClassVar[LineKind]
ACCOUNT_KIND_UNSPECIFIED: AccountKind
ACCOUNT_KIND_CREDIT_CARD: AccountKind
ACCOUNT_KIND_DEPOSIT: AccountKind
ACCOUNT_KIND_SECURITIES: AccountKind
ACCOUNT_KIND_POINTS: AccountKind
ACCOUNT_KIND_LOAN: AccountKind
LINE_KIND_UNSPECIFIED: LineKind
LINE_KIND_PURCHASE: LineKind
LINE_KIND_FOREIGN_TRANSACTION_FEE: LineKind
LINE_KIND_ANNUAL_FEE: LineKind
LINE_KIND_REFUND: LineKind
LINE_KIND_REBATE: LineKind
LINE_KIND_PAYMENT: LineKind
LINE_KIND_INTEREST: LineKind
LINE_KIND_FEE: LineKind
LINE_KIND_OTHER: LineKind
LINE_KIND_BUY: LineKind
LINE_KIND_SELL: LineKind
LINE_KIND_EARN: LineKind
LINE_KIND_ADJUST: LineKind
LINE_KIND_REDEEM: LineKind

class DescribeParsersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class DescribeParsersResponse(_message.Message):
    __slots__ = ("parsers", "instance")
    PARSERS_FIELD_NUMBER: _ClassVar[int]
    INSTANCE_FIELD_NUMBER: _ClassVar[int]
    parsers: _containers.RepeatedCompositeFieldContainer[ParserVersion]
    instance: str
    def __init__(self, parsers: _Optional[_Iterable[_Union[ParserVersion, _Mapping]]] = ..., instance: _Optional[str] = ...) -> None: ...

class ParserVersion(_message.Message):
    __slots__ = ("name", "version")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    name: str
    version: str
    def __init__(self, name: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class ParseDocumentRequest(_message.Message):
    __slots__ = ("document_id", "content")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    content: bytes
    def __init__(self, document_id: _Optional[str] = ..., content: _Optional[bytes] = ...) -> None: ...

class OpenDocumentRequest(_message.Message):
    __slots__ = ("document_id", "content")
    DOCUMENT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    document_id: str
    content: bytes
    def __init__(self, document_id: _Optional[str] = ..., content: _Optional[bytes] = ...) -> None: ...

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
    __slots__ = ("parser", "parser_version", "statement", "warnings")
    PARSER_FIELD_NUMBER: _ClassVar[int]
    PARSER_VERSION_FIELD_NUMBER: _ClassVar[int]
    STATEMENT_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    parser: str
    parser_version: str
    statement: Statement
    warnings: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, parser: _Optional[str] = ..., parser_version: _Optional[str] = ..., statement: _Optional[_Union[Statement, _Mapping]] = ..., warnings: _Optional[_Iterable[str]] = ...) -> None: ...

class Statement(_message.Message):
    __slots__ = ("institution", "period_start", "closing_date", "accounts", "checks", "card_bill")
    INSTITUTION_FIELD_NUMBER: _ClassVar[int]
    PERIOD_START_FIELD_NUMBER: _ClassVar[int]
    CLOSING_DATE_FIELD_NUMBER: _ClassVar[int]
    ACCOUNTS_FIELD_NUMBER: _ClassVar[int]
    CHECKS_FIELD_NUMBER: _ClassVar[int]
    CARD_BILL_FIELD_NUMBER: _ClassVar[int]
    institution: str
    period_start: _date_pb2.Date
    closing_date: _date_pb2.Date
    accounts: _containers.RepeatedCompositeFieldContainer[AccountReport]
    checks: _containers.RepeatedCompositeFieldContainer[Check]
    card_bill: CardBill
    def __init__(self, institution: _Optional[str] = ..., period_start: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., closing_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., accounts: _Optional[_Iterable[_Union[AccountReport, _Mapping]]] = ..., checks: _Optional[_Iterable[_Union[Check, _Mapping]]] = ..., card_bill: _Optional[_Union[CardBill, _Mapping]] = ...) -> None: ...

class CardBill(_message.Message):
    __slots__ = ("due_date", "currency", "total_due")
    DUE_DATE_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    TOTAL_DUE_FIELD_NUMBER: _ClassVar[int]
    due_date: _date_pb2.Date
    currency: str
    total_due: _decimal_pb2.Decimal
    def __init__(self, due_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., currency: _Optional[str] = ..., total_due: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...

class AccountReport(_message.Message):
    __slots__ = ("kind", "number", "name", "balances", "lines", "expiries", "lapse_rules")
    KIND_FIELD_NUMBER: _ClassVar[int]
    NUMBER_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    BALANCES_FIELD_NUMBER: _ClassVar[int]
    LINES_FIELD_NUMBER: _ClassVar[int]
    EXPIRIES_FIELD_NUMBER: _ClassVar[int]
    LAPSE_RULES_FIELD_NUMBER: _ClassVar[int]
    kind: AccountKind
    number: str
    name: str
    balances: _containers.RepeatedCompositeFieldContainer[Balance]
    lines: _containers.RepeatedCompositeFieldContainer[Line]
    expiries: _containers.RepeatedCompositeFieldContainer[Expiry]
    lapse_rules: _containers.RepeatedCompositeFieldContainer[LapseRule]
    def __init__(self, kind: _Optional[_Union[AccountKind, str]] = ..., number: _Optional[str] = ..., name: _Optional[str] = ..., balances: _Optional[_Iterable[_Union[Balance, _Mapping]]] = ..., lines: _Optional[_Iterable[_Union[Line, _Mapping]]] = ..., expiries: _Optional[_Iterable[_Union[Expiry, _Mapping]]] = ..., lapse_rules: _Optional[_Iterable[_Union[LapseRule, _Mapping]]] = ...) -> None: ...

class Balance(_message.Message):
    __slots__ = ("unit", "as_of", "amount")
    UNIT_FIELD_NUMBER: _ClassVar[int]
    AS_OF_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    unit: str
    as_of: _date_pb2.Date
    amount: _decimal_pb2.Decimal
    def __init__(self, unit: _Optional[str] = ..., as_of: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...

class Line(_message.Message):
    __slots__ = ("date", "unit", "amount", "kind", "description", "card", "deposit", "trade")
    DATE_FIELD_NUMBER: _ClassVar[int]
    UNIT_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    CARD_FIELD_NUMBER: _ClassVar[int]
    DEPOSIT_FIELD_NUMBER: _ClassVar[int]
    TRADE_FIELD_NUMBER: _ClassVar[int]
    date: _date_pb2.Date
    unit: str
    amount: _decimal_pb2.Decimal
    kind: LineKind
    description: str
    card: CardLine
    deposit: DepositLine
    trade: Trade
    def __init__(self, date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., unit: _Optional[str] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., kind: _Optional[_Union[LineKind, str]] = ..., description: _Optional[str] = ..., card: _Optional[_Union[CardLine, _Mapping]] = ..., deposit: _Optional[_Union[DepositLine, _Mapping]] = ..., trade: _Optional[_Union[Trade, _Mapping]] = ...) -> None: ...

class CardLine(_message.Message):
    __slots__ = ("posting_date", "original_amount", "original_currency", "country", "card_last4")
    POSTING_DATE_FIELD_NUMBER: _ClassVar[int]
    ORIGINAL_AMOUNT_FIELD_NUMBER: _ClassVar[int]
    ORIGINAL_CURRENCY_FIELD_NUMBER: _ClassVar[int]
    COUNTRY_FIELD_NUMBER: _ClassVar[int]
    CARD_LAST4_FIELD_NUMBER: _ClassVar[int]
    posting_date: _date_pb2.Date
    original_amount: _decimal_pb2.Decimal
    original_currency: str
    country: str
    card_last4: str
    def __init__(self, posting_date: _Optional[_Union[_date_pb2.Date, _Mapping]] = ..., original_amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., original_currency: _Optional[str] = ..., country: _Optional[str] = ..., card_last4: _Optional[str] = ...) -> None: ...

class DepositLine(_message.Message):
    __slots__ = ("memo", "counterparty", "balance")
    MEMO_FIELD_NUMBER: _ClassVar[int]
    COUNTERPARTY_FIELD_NUMBER: _ClassVar[int]
    BALANCE_FIELD_NUMBER: _ClassVar[int]
    memo: str
    counterparty: str
    balance: _decimal_pb2.Decimal
    def __init__(self, memo: _Optional[str] = ..., counterparty: _Optional[str] = ..., balance: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...

class Trade(_message.Message):
    __slots__ = ("price", "category", "amount", "fee", "tax", "net", "prepaid")
    PRICE_FIELD_NUMBER: _ClassVar[int]
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    FEE_FIELD_NUMBER: _ClassVar[int]
    TAX_FIELD_NUMBER: _ClassVar[int]
    NET_FIELD_NUMBER: _ClassVar[int]
    PREPAID_FIELD_NUMBER: _ClassVar[int]
    price: _decimal_pb2.Decimal
    category: str
    amount: _decimal_pb2.Decimal
    fee: _decimal_pb2.Decimal
    tax: _decimal_pb2.Decimal
    net: _decimal_pb2.Decimal
    prepaid: _decimal_pb2.Decimal
    def __init__(self, price: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., category: _Optional[str] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., fee: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., tax: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., net: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., prepaid: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ...) -> None: ...

class Expiry(_message.Message):
    __slots__ = ("unit", "amount", "by")
    UNIT_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    BY_FIELD_NUMBER: _ClassVar[int]
    unit: str
    amount: _decimal_pb2.Decimal
    by: _date_pb2.Date
    def __init__(self, unit: _Optional[str] = ..., amount: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., by: _Optional[_Union[_date_pb2.Date, _Mapping]] = ...) -> None: ...

class LapseRule(_message.Message):
    __slots__ = ("unit", "months_after_closing", "yearly_purge")
    UNIT_FIELD_NUMBER: _ClassVar[int]
    MONTHS_AFTER_CLOSING_FIELD_NUMBER: _ClassVar[int]
    YEARLY_PURGE_FIELD_NUMBER: _ClassVar[int]
    unit: str
    months_after_closing: int
    yearly_purge: YearlyPurge
    def __init__(self, unit: _Optional[str] = ..., months_after_closing: _Optional[int] = ..., yearly_purge: _Optional[_Union[YearlyPurge, _Mapping]] = ...) -> None: ...

class YearlyPurge(_message.Message):
    __slots__ = ("last_month", "years")
    LAST_MONTH_FIELD_NUMBER: _ClassVar[int]
    YEARS_FIELD_NUMBER: _ClassVar[int]
    last_month: int
    years: int
    def __init__(self, last_month: _Optional[int] = ..., years: _Optional[int] = ...) -> None: ...

class Check(_message.Message):
    __slots__ = ("name", "unit", "computed", "stated", "balanced")
    NAME_FIELD_NUMBER: _ClassVar[int]
    UNIT_FIELD_NUMBER: _ClassVar[int]
    COMPUTED_FIELD_NUMBER: _ClassVar[int]
    STATED_FIELD_NUMBER: _ClassVar[int]
    BALANCED_FIELD_NUMBER: _ClassVar[int]
    name: str
    unit: str
    computed: _decimal_pb2.Decimal
    stated: _decimal_pb2.Decimal
    balanced: bool
    def __init__(self, name: _Optional[str] = ..., unit: _Optional[str] = ..., computed: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., stated: _Optional[_Union[_decimal_pb2.Decimal, _Mapping]] = ..., balanced: _Optional[bool] = ...) -> None: ...
