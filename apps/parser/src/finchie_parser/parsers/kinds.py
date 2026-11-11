"""What kind of line a statement line is, by the bank's own words (ADR 0013): one set of rules for every bank,
so that the same line gets the same kind whichever bank prints it.

A parser classifies by layout first where its layout says more than words can (a payments section, a
row with no card), and by these words otherwise.
"""

from decimal import Decimal

from finchie_parser.model import LineKind

# A foreign transaction fee, or one given back with a refund (國外退貨手續費).
_FOREIGN_TRANSACTION_FEE = ("國外交易服務費", "國外交易手續費", "國外退貨手續費")
# An annual fee, or its waiver (negative).
_ANNUAL_FEE = ("年費",)
# Cash back (…回饋) and points redeemed against the bill (點數折抵…), as parser.proto defines LINE_KIND_REBATE.
_REBATE = ("回饋", "折抵")
# A payment, as the description words it: 扣繳, 自動扣款, 繳款, and older wordings such as 您的付款已收到.
_PAYMENT = ("扣繳", "繳款", "自動扣款", "付款已收到")
# Interest and the bank's other charges. No kind fits them: they count as purchases, as their sign says.
_CHARGES = ("利息", "循環", "違約金", "其他費用")
# A deposit account's interest, as the banks word the whole description.
_DEPOSIT_INTEREST = frozenset({"利息", "利息存入", "存款息", "INTEREST"})


def card_kind_by_words(description: str, amount: Decimal) -> LineKind | None:
    """The kind a card bill line's description names, if it names one."""
    if _has(description, _FOREIGN_TRANSACTION_FEE):
        return LineKind.FOREIGN_TRANSACTION_FEE
    if _has(description, _ANNUAL_FEE):
        return LineKind.ANNUAL_FEE
    if amount < 0 and _has(description, _PAYMENT):
        return LineKind.PAYMENT
    if _has(description, _REBATE):
        return LineKind.REBATE
    return None


def card_kind(description: str, amount: Decimal) -> LineKind:
    """A card bill line's kind: the one its description names, else a refund or a purchase by its sign."""
    return card_kind_by_words(description, amount) or by_sign(amount)


def by_sign(amount: Decimal) -> LineKind:
    return LineKind.REFUND if amount < 0 else LineKind.PURCHASE


def is_charge(description: str) -> bool:
    """Interest or another of the bank's charges, which `card_kind` counts as a purchase."""
    return _has(description, _CHARGES)


def deposit_kind(description: str) -> LineKind:
    """A deposit line's kind, by its whole description: only what the bank's wording says (ADR 0013)."""
    return LineKind.INTEREST if description in _DEPOSIT_INTEREST else LineKind.OTHER


def _has(description: str, words: tuple[str, ...]) -> bool:
    return any(word in description for word in words)
