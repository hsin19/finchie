from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.cathay import CathayParser
from finchie_parser.parsers.esun import EsunParser
from finchie_parser.parsers.esun_deposit import EsunDepositParser
from finchie_parser.parsers.hsbc import HsbcParser
from finchie_parser.parsers.taishin import TaishinParser

PARSERS: tuple[StatementParser, ...] = (
    EsunParser(),
    EsunDepositParser(),
    TaishinParser(),
    CathayParser(),
    HsbcParser(),
)

__all__ = ["PARSERS", "Parsed", "StatementParser"]
