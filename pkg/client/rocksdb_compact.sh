# Compacts every column family of every RocksDB database under a datadir, with
# the options each database was written with, for clients that ship no offline
# compactor (nethermind, besu). The client must be stopped.
#
#   sh -c "$(cat rocksdb_compact.sh)" rocksdb-compact <datadir> [ldb option]...
#
# Extra arguments are passed to every `ldb compact`.
set -eu

[ $# -ge 1 ] || { echo "usage: rocksdb-compact <datadir> [ldb option]..." >&2; exit 2; }
root=$1
shift

nl='
'

# A column family name is printed as hex when it is not printable: besu names
# its families by single bytes (0x01..0x12), one of which is a newline.
label() {
	case $1 in
	*[![:print:]]* | '') printf '0x%s' "$(printf %s "$1" | od -An -tx1 | tr -d ' \n')" ;;
	*) printf %s "$1" ;;
	esac
}

# levels <db> <cf>: the family's non-empty levels as L<n>=<files>/<MB>, then
# its SST bytes. Logged before and after, so the run output shows what the
# compaction did, not only that it ran.
levels() {
	ldb --db="$1" --try_load_options --column_family="$2" get_property rocksdb.levelstats </dev/null |
		awk 'NR > 2 && $2 > 0 { printf "L%s=%s/%sMB ", $1, $2, $3 }'
	ldb --db="$1" --try_load_options --column_family="$2" get_property rocksdb.total-sst-files-size </dev/null |
		sed 's/.*: /sst_bytes=/'
}

dbs=$(find "$root" -type f -name 'OPTIONS-*' -exec dirname {} \; | sort -u)
[ -n "$dbs" ] || { echo "no RocksDB database (no OPTIONS-* file) under $root" >&2; exit 1; }

echo "$dbs" | while IFS= read -r db; do
	# ldb opens a family that has no merge operator with a string-append one
	# (and records it in OPTIONS), so a family with a merge operator of its own
	# would have its operands merged wrongly.
	opts=$(ls "$db"/OPTIONS-* | sort -V | tail -n 1)
	foreign=$(grep -E '^[[:space:]]*merge_operator=' "$opts" |
		grep -vxE '[[:space:]]*merge_operator=(nullptr|\{id=StringAppendOperator;delimiter=\\:;\})' || true)
	if [ -n "$foreign" ]; then
		echo "$db: refusing to compact: $opts sets a merge operator ldb does not have:$nl$foreign" >&2
		exit 1
	fi

	# ldb prints `Column families in <db>: ` and then `{a, b, c}`, names raw,
	# and exits 0 even when it cannot read the database.
	out=$(ldb --db="$db" list_column_families </dev/null)
	case $out in
	*": $nl{"*"}") ;;
	*) echo "$db: cannot list column families: $out" >&2; exit 1 ;;
	esac
	cfs=${out#*": $nl{"}
	cfs=${cfs%"}"}

	while :; do
		case $cfs in
		*", "*) cf=${cfs%%", "*}; cfs=${cfs#*", "} ;;
		*) cf=$cfs; cfs= ;;
		esac

		echo "compacting $db column_family=$(label "$cf") $(levels "$db" "$cf")"
		start=$(date +%s)
		ldb --db="$db" --try_load_options --column_family="$cf" "$@" compact </dev/null

		# `compact` falls back to the default family for an unknown name and
		# still exits 0; get_property does not, and an empty level 0 is what a
		# finished full compaction leaves.
		after=$(levels "$db" "$cf")
		case $after in
		*"L0="*) echo "$db column_family=$(label "$cf"): level 0 not empty after compaction: $after" >&2; exit 1 ;;
		esac
		echo "compacted $db column_family=$(label "$cf") $after duration=$(( $(date +%s) - start ))s"

		[ -n "$cfs" ] || break
	done
done
