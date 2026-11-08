from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.cathay import CathayParser
from finchie_parser.parsers.ctbc_deposit import CtbcDepositParser
from finchie_parser.parsers.esun import EsunParser
from finchie_parser.parsers.esun_deposit import EsunDepositParser
from finchie_parser.parsers.fubon_deposit import FubonDepositParser
from finchie_parser.parsers.hsbc import HsbcParser
from finchie_parser.parsers.nextbank_deposit import NextBankDepositParser
from finchie_parser.parsers.skis_securities import SkisSecuritiesParser
from finchie_parser.parsers.taishin import TaishinParser
from finchie_parser.parsers.taishin_deposit import TaishinDepositParser

PARSERS: tuple[StatementParser, ...] = (
    EsunParser(),
    EsunDepositParser(),
    TaishinParser(),
    TaishinDepositParser(),
    CathayParser(),
    HsbcParser(),
    FubonDepositParser(),
    NextBankDepositParser(),
    CtbcDepositParser(),
    SkisSecuritiesParser(),
)

__all__ = ["PARSERS", "Parsed", "StatementParser"]
