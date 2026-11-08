"""The real parsers' claims and password rules, against the senders and subjects banks use (ADR 0014)."""

from datetime import date

import pytest
from pdfs import email_with

from finchie_parser.documents import read_mail
from finchie_parser.mail_filter import LABEL, MAILBOX_PLACEHOLDER, gmail_filter
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.passwords import Identity, MissingDetailError
from finchie_parser.pipeline import claimants

# Synthetic details (ADR 0006).
IDENTITY = Identity(national_id="a123456789", birth_date=date(2000, 1, 2))


@pytest.mark.parametrize(
    ("sender", "subject", "parser"),
    [
        ("estatement@esunbank.com", "玉山銀行2026年09月信用卡電子帳單", "esun.credit_card"),
        ("estatement@esunbank.com", "玉山銀行2026年09月綜合對帳單", "esun.deposit"),
        ("webmaster@bhurecv.taishinbank.com.tw", "台新信用卡電子帳單 2026年10月", "taishin.credit_card"),
        ("webmaster@bhurecv.taishinbank.com.tw", "台新銀行綜合對帳單 2026年9月份", "taishin.deposit"),
        ("service@pxbillrc01.cathaybk.com.tw", "國泰世華銀行信用卡2026年11月電子帳單", "cathay.credit_card"),
        ("cards@estatements.hsbc.com.tw", "115年10月滙豐某某卡信用卡帳單", "hsbc.credit_card"),
        ("service@bhu.taipeifubon.com.tw", "台北富邦銀行2026年10月 銀行對帳單", "fubon.deposit"),
        ("estatement@esunbank.com.example", "玉山銀行2026年09月信用卡電子帳單", None),
        ("statements@bank.example", "範例銀行 2026年11月 信用卡電子帳單", None),
    ],
)
def test_one_parser_at_most_claims_each_kind_of_email(sender: str, subject: str, parser: str | None) -> None:
    mail = read_mail(email_with(sender=sender, subject=subject))
    assert [p.name for p in claimants(mail, PARSERS)] == ([parser] if parser else [])


def by_name(name: str) -> StatementParser:
    return next(parser for parser in PARSERS if parser.name == name)


@pytest.mark.parametrize(
    ("parser", "passwords"),
    [
        ("esun.credit_card", []),
        ("esun.deposit", ["A123456789"]),
        ("taishin.credit_card", ["890102", "A123456789"]),
        ("taishin.deposit", ["A123456789"]),
        ("cathay.credit_card", ["A123456789"]),
        ("hsbc.credit_card", ["A123456789"]),
        ("fubon.deposit", ["A123456789"]),
    ],
)
def test_each_banks_password_rule(parser: str, passwords: list[str]) -> None:
    assert by_name(parser).passwords(IDENTITY) == passwords


def test_a_rule_without_its_detail_names_the_variable() -> None:
    with pytest.raises(MissingDetailError) as error:
        by_name("taishin.credit_card").passwords(Identity(national_id="A123456789"))
    assert error.value.variable == "PARSER_IDENTITY__BIRTH_DATE"


def test_the_gmail_filter_forwards_every_claimed_domain_to_the_placeholder() -> None:
    xml = gmail_filter()
    for parser in PARSERS:
        for domain in parser.sender_domains:
            assert domain in xml
    assert f'value="{MAILBOX_PLACEHOLDER}"' in xml
    assert f'value="{LABEL}"' in xml
