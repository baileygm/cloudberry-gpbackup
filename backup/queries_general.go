package backup

/*
 * This file contains structs and functions related to executing specific
 * queries to gather metadata for the objects handled in predata_general.go.
 */

import (
	"fmt"

	"github.com/greenplum-db/gpbackup/utils"
)

/*
 * Queries requiring their own structs
 */

func GetAllUserSchemas(connection *utils.DBConn) []Schema {
	/*
	 * This query is constructed from scratch, but the list of schemas to exclude
	 * is copied from gpcrondump so that gpbackup exhibits similar behavior regarding
	 * which schemas are dumped.
	 */
	query := fmt.Sprintf(`
SELECT
	oid,
	nspname AS name
FROM pg_namespace n
WHERE %s
ORDER BY name;`, NonUserSchemaFilterClause("n"))
	results := make([]Schema, 0)

	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QueryConstraint struct {
	Oid                uint32
	ConName            string
	ConType            string
	ConDef             string
	OwningObject       string
	IsDomainConstraint bool
	IsPartitionParent  bool
}

func GetConstraints(connection *utils.DBConn) []QueryConstraint {
	// This query is adapted from the queries underlying \d in psql.
	query := fmt.Sprintf(`
SELECT
	c.oid,
	conname,
	contype,
	pg_get_constraintdef(c.oid, TRUE) AS condef,
	CASE
		WHEN r.relname IS NULL THEN quote_ident(n.nspname) || '.' ||quote_ident(t.typname)
		ELSE  quote_ident(n.nspname) || '.' || quote_ident(r.relname)
	END AS owningobject,
	CASE
		WHEN r.relname IS NULL THEN 't'
		ELSE 'f'
	END AS isdomainconstraint,
	CASE
		WHEN pt.parrelid IS NULL THEN 'f'
		ELSE 't'
	END AS ispartitionparent
FROM pg_constraint c
LEFT JOIN pg_class r
	ON c.conrelid = r.oid
LEFT JOIN pg_partition_rule pr
	ON c.conrelid = pr.parchildrelid
LEFT JOIN pg_partition pt
	ON c.conrelid = pt.parrelid
LEFT JOIN pg_type t
	ON c.contypid = t.oid
JOIN pg_namespace n
	ON n.oid = c.connamespace
WHERE %s
AND pr.parchildrelid IS NULL
ORDER BY conname;`, NonUserSchemaFilterClause("n"))

	results := make([]QueryConstraint, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

func GetAllSequenceRelations(connection *utils.DBConn) []Relation {
	query := `SELECT
	n.oid AS schemaoid,
	c.oid AS relationoid,
	n.nspname AS schemaname,
	c.relname AS relationname
FROM pg_class c
LEFT JOIN pg_namespace n
	ON c.relnamespace = n.oid
WHERE relkind = 'S'
ORDER BY schemaname, relationname;`

	results := make([]Relation, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QuerySequenceDefinition struct {
	Name      string `db:"sequence_name"`
	LastVal   int64  `db:"last_value"`
	Increment int64  `db:"increment_by"`
	MaxVal    int64  `db:"max_value"`
	MinVal    int64  `db:"min_value"`
	CacheVal  int64  `db:"cache_value"`
	LogCnt    int64  `db:"log_cnt"`
	IsCycled  bool   `db:"is_cycled"`
	IsCalled  bool   `db:"is_called"`
}

func GetSequenceDefinition(connection *utils.DBConn, seqName string) QuerySequenceDefinition {
	query := fmt.Sprintf("SELECT * FROM %s", seqName)
	result := QuerySequenceDefinition{}
	err := connection.Get(&result, query)
	utils.CheckError(err)
	return result
}

type QuerySequenceOwner struct {
	SchemaName   string `db:"nspname"`
	SequenceName string
	TableName    string
	ColumnName   string `db:"attname"`
}

func GetSequenceColumnOwnerMap(connection *utils.DBConn) map[string]string {
	query := `SELECT
	n.nspname,
	s.relname AS sequencename,
	t.relname AS tablename,
	a.attname
FROM pg_depend d
JOIN pg_attribute a
	ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
JOIN pg_class s
	ON s.oid = d.objid
JOIN pg_class t
	ON t.oid = d.refobjid
JOIN pg_namespace n
	ON n.oid = s.relnamespace
WHERE s.relkind = 'S';`

	results := make([]QuerySequenceOwner, 0)
	sequenceOwners := make(map[string]string, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	for _, seqOwner := range results {
		seqFQN := MakeFQN(seqOwner.SchemaName, seqOwner.SequenceName)
		columnFQN := fmt.Sprintf("%s.%s", MakeFQN(seqOwner.SchemaName, seqOwner.TableName), utils.QuoteIdent(seqOwner.ColumnName))
		sequenceOwners[seqFQN] = columnFQN
	}
	return sequenceOwners
}

type QuerySessionGUCs struct {
	ClientEncoding       string `db:"client_encoding"`
	StdConformingStrings string `db:"standard_conforming_strings"`
	DefaultWithOids      string `db:"default_with_oids"`
}

func GetSessionGUCs(connection *utils.DBConn) QuerySessionGUCs {
	result := QuerySessionGUCs{}
	query := "SHOW client_encoding;"
	err := connection.Get(&result, query)
	query = "SHOW standard_conforming_strings;"
	err = connection.Get(&result, query)
	query = "SHOW default_with_oids;"
	err = connection.Get(&result, query)
	utils.CheckError(err)
	return result
}

type QueryProceduralLanguage struct {
	Oid       uint32
	Name      string `db:"lanname"`
	Owner     string
	IsPl      bool   `db:"lanispl"`
	PlTrusted bool   `db:"lanpltrusted"`
	Handler   uint32 `db:"lanplcallfoid"`
	Inline    uint32 `db:"laninline"`
	Validator uint32 `db:"lanvalidator"`
}

func GetProceduralLanguages(connection *utils.DBConn) []QueryProceduralLanguage {
	results := make([]QueryProceduralLanguage, 0)
	query := `
SELECT
	oid,
	l.lanname,
	pg_get_userbyid(l.lanowner) as owner,
	l.lanispl,
	l.lanpltrusted,
	l.lanplcallfoid::regprocedure::oid,
	l.laninline::regprocedure::oid,
	l.lanvalidator::regprocedure::oid
FROM pg_language l
WHERE l.lanispl='t';
`
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QueryExtProtocol struct {
	Oid           uint32
	Name          string `db:"ptcname"`
	Owner         string
	Trusted       bool   `db:"ptctrusted"`
	ReadFunction  uint32 `db:"ptcreadfn"`
	WriteFunction uint32 `db:"ptcwritefn"`
	Validator     uint32 `db:"ptcvalidatorfn"`
}

func GetExternalProtocols(connection *utils.DBConn) []QueryExtProtocol {
	results := make([]QueryExtProtocol, 0)
	query := `
SELECT
	p.oid,
	p.ptcname,
	pg_get_userbyid(p.ptcowner) as owner,
	p.ptctrusted,
	p.ptcreadfn,
	p.ptcwritefn,
	p.ptcvalidatorfn
FROM pg_extprotocol p;
`
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QueryOperator struct {
	Oid              uint32
	SchemaName       string
	Name             string
	ProcedureName    string
	LeftArgType      string
	RightArgType     string
	CommutatorOp     string
	NegatorOp        string
	RestrictFunction string
	JoinFunction     string
	CanHash          bool
	CanMerge         bool
}

func GetOperators(connection *utils.DBConn) []QueryOperator {
	results := make([]QueryOperator, 0)
	query := fmt.Sprintf(`
SELECT
	o.oid,
	n.nspname AS schemaname,
	oprname AS name,
	oprcode AS procedurename,
	oprleft::regtype AS leftargtype,
	oprright::regtype AS rightargtype,
	oprcom::regoper AS commutatorop,
	oprnegate::regoper AS negatorop,
	oprrest AS restrictfunction,
	oprjoin AS joinfunction,
	oprcanmerge AS canmerge,
	oprcanhash AS canhash
FROM pg_operator o
JOIN pg_namespace n on n.oid = o.oprnamespace
WHERE %s AND oprcode != 0`, NonUserSchemaFilterClause("n"))
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QueryOperatorFamily struct {
	Oid         uint32
	SchemaName  string
	Name        string
	IndexMethod string
}

func GetOperatorFamilies(connection *utils.DBConn) []QueryOperatorFamily {
	results := make([]QueryOperatorFamily, 0)
	query := fmt.Sprintf(`
SELECT
	o.oid,
	n.nspname AS schemaname,
	opfname AS name,
	(SELECT amname FROM pg_am WHERE oid = opfmethod) AS indexMethod
FROM pg_opfamily o
JOIN pg_namespace n on n.oid = o.opfnamespace
WHERE %s`, NonUserSchemaFilterClause("n"))
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type QueryOperatorClass struct {
	Oid          uint32
	ClassSchema  string
	ClassName    string
	FamilySchema string
	FamilyName   string
	IndexMethod  string
	Type         string
	Default      bool
	StorageType  string
	Operators    []OperatorClassOperator
	Functions    []OperatorClassFunction
}

func GetOperatorClasses(connection *utils.DBConn) []QueryOperatorClass {
	results := make([]QueryOperatorClass, 0)
	query := fmt.Sprintf(`
SELECT
	c.oid,
	cls_ns.nspname AS classschema,
	opcname AS classname,
	fam_ns.nspname AS familyschema,
	opfname AS familyname,
	(SELECT amname FROM pg_catalog.pg_am WHERE oid = opcmethod) AS indexmethod,
	opcintype::pg_catalog.regtype AS type,
	opcdefault AS default,
	opckeytype::pg_catalog.regtype AS storagetype
FROM pg_catalog.pg_opclass c
LEFT JOIN pg_catalog.pg_opfamily f ON f.oid = opcfamily
JOIN pg_catalog.pg_namespace cls_ns ON cls_ns.oid = opcnamespace
JOIN pg_catalog.pg_namespace fam_ns ON fam_ns.oid = opfnamespace
WHERE %s`, NonUserSchemaFilterClause("cls_ns"))
	err := connection.Select(&results, query)
	utils.CheckError(err)

	operators := GetOperatorClassOperators(connection)
	for i := range results {
		results[i].Operators = operators[results[i].Oid]
	}
	functions := GetOperatorClassFunctions(connection)
	for i := range results {
		results[i].Functions = functions[results[i].Oid]
	}

	return results
}

type OperatorClassOperator struct {
	ClassOid       uint32
	StrategyNumber int
	Operator       string
	Recheck        bool
}

func GetOperatorClassOperators(connection *utils.DBConn) map[uint32][]OperatorClassOperator {
	results := make([]OperatorClassOperator, 0)
	query := fmt.Sprintf(`
SELECT
	refobjid AS classoid,
	amopstrategy AS strategynumber,
	amopopr::pg_catalog.regoperator AS operator,
	amopreqcheck AS recheck
FROM pg_catalog.pg_amop ao, pg_catalog.pg_depend
WHERE refclassid = 'pg_catalog.pg_opclass'::pg_catalog.regclass
AND classid = 'pg_catalog.pg_amop'::pg_catalog.regclass
AND objid = ao.oid
ORDER BY amopstrategy
`)
	err := connection.Select(&results, query)
	utils.CheckError(err)

	operators := make(map[uint32][]OperatorClassOperator, 0)
	for _, result := range results {
		operators[result.ClassOid] = append(operators[result.ClassOid], result)
	}
	return operators
}

type OperatorClassFunction struct {
	ClassOid      uint32
	SupportNumber int
	FunctionName  string
}

func GetOperatorClassFunctions(connection *utils.DBConn) map[uint32][]OperatorClassFunction {
	results := make([]OperatorClassFunction, 0)
	query := fmt.Sprintf(`
SELECT
	refobjid AS classoid,
	amprocnum AS supportnumber,
	amproc::pg_catalog.regprocedure AS functionname
FROM pg_catalog.pg_amproc ap, pg_catalog.pg_depend
WHERE refclassid = 'pg_catalog.pg_opclass'::pg_catalog.regclass
AND classid = 'pg_catalog.pg_amproc'::pg_catalog.regclass
AND objid = ap.oid
ORDER BY amprocnum
`)
	err := connection.Select(&results, query)
	utils.CheckError(err)

	functions := make(map[uint32][]OperatorClassFunction, 0)
	for _, result := range results {
		functions[result.ClassOid] = append(functions[result.ClassOid], result)
	}
	return functions
}

type Conversion struct {
	Oid                uint32
	Schema             string `db:"nspname"`
	Name               string `db:"conname"`
	ForEncoding        string
	ToEncoding         string
	ConversionFunction string
	IsDefault          bool `db:"condefault"`
}

func GetConversions(connection *utils.DBConn) []Conversion {
	results := make([]Conversion, 0)
	query := fmt.Sprintf(`
SELECT
	c.oid,
	n.nspname,
	c.conname,
	pg_encoding_to_char(c.conforencoding) AS forencoding,
	pg_encoding_to_char(c.contoencoding) AS toencoding,
	quote_ident(fn.nspname) || '.' || quote_ident(p.proname) AS conversionfunction,
	c.condefault
FROM pg_conversion c
JOIN pg_namespace n ON c.connamespace = n.oid
JOIN pg_proc p ON c.conproc = p.oid
JOIN pg_namespace fn ON p.pronamespace = fn.oid
WHERE %s
ORDER BY n.nspname, c.conname;`, NonUserSchemaFilterClause("n"))

	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}
