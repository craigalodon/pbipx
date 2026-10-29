package sqlfile

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/shape"
)

func describe(t *testing.T, src string) *Description {
	t.Helper()
	d, err := Describe([]byte(src))
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	return d
}

func TestDescribeRelations(t *testing.T) {
	d := describe(t, `create or replace temporary view analytics.roster as
with active as (select * from warehouse.dbo.enrollment), recent (id) as (select 1)
select * from active a, staging_roster r join warehouse.dbo.student s on s.id = a.id;
create table if not exists analytics.snapshot as select * from recent;
insert overwrite table analytics.history select * from warehouse.dbo.term;
insert into analytics.log select * from (select * from warehouse.dbo.section) x;
merge into analytics.student t using warehouse.dbo.student_update u on t.id = u.id
when matched then update set t.name = u.name;
delete from analytics.stale where id = 1;
create schema analytics;`)
	wantCreates := []Created{{Kind: "view", Name: "analytics.roster"}, {Kind: "table", Name: "analytics.snapshot"}}
	if !slices.Equal(d.Creates, wantCreates) {
		t.Errorf("creates = %v, want %v", d.Creates, wantCreates)
	}
	check := func(what string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", what, got, want)
		}
	}
	check("writes", d.Writes, []string{"analytics.history", "analytics.log", "analytics.stale", "analytics.student"})
	check("reads", d.Reads, []string{
		"warehouse.dbo.enrollment", "warehouse.dbo.section", "warehouse.dbo.student",
		"warehouse.dbo.student_update", "warehouse.dbo.term",
	})
	check("reads_unqualified", d.ReadsUnqualified, []string{"active", "recent", "staging_roster"})
}

func TestDescribeFromThatIsNotARelation(t *testing.T) {
	d := describe(t, `select extract(year from d), trim(both from n), substring(s from 1 for 2),
SUBSTR(s FROM 3), overlay(s placing t from 2)
from warehouse.dbo.enrollment
where a is distinct from b and c is not distinct from e`)
	if !slices.Equal(d.Reads, []string{"warehouse.dbo.enrollment"}) || len(d.ReadsUnqualified) != 0 {
		t.Errorf("reads = %q, unqualified = %q; want the one table", d.Reads, d.ReadsUnqualified)
	}
}

func TestDescribeTemplateCalls(t *testing.T) {
	d := describe(t, `{{ config(materialized='view') }}
{# built from {{ ref('legacy') }} #}
-- see source('raw', 'term')
select * from {{ ref('stg_student') }} s
join {{ source("raw", "enrollment") }} e on e.id = s.id
join {{ ref(var('m')) }} v on v.id = s.id
join {{ ref('Doe, Jane') }} w on w.id = s.id
join {{ source(schema, 'x') }} y on y.id = s.id`)
	want := []string{shape.Dynamic, shape.NotIdentifier, "legacy", "raw.enrollment", "raw.term", "stg_student"}
	if !slices.Equal(d.Reads, want) {
		t.Errorf("reads = %q, want %q", d.Reads, want)
	}
}

func TestDescribeNamesThatAreNotIdentifiers(t *testing.T) {
	d := describe(t, "select * from warehouse.\"Doe, Jane\" join `roster 2105` r on r.id = 1 join \"2105\".dbo.t z on z.id = 1")
	want := []string{shape.NotIdentifier + ".dbo.t", "warehouse." + shape.NotIdentifier}
	if !slices.Equal(d.Reads, want) || !slices.Equal(d.ReadsUnqualified, []string{shape.NotIdentifier}) {
		t.Errorf("reads = %q, unqualified = %q", d.Reads, d.ReadsUnqualified)
	}
}

func TestDescribeCountsLiteralsNotComments(t *testing.T) {
	d := describe(t, "-- 'Doe, Jane'\nselect * from t where a = 'x' and b = 2")
	if len(d.Literals) != 1 || d.Literals[string(finding.SQL)] != 2 {
		t.Errorf("literals = %v, want sql: 2", d.Literals)
	}
}

func TestDescribeEmptyListsAreEmpty(t *testing.T) {
	out, err := json.Marshal(describe(t, "select 1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "null") {
		t.Errorf("an empty list is written as null")
	}
}

func TestDescribeUnlexable(t *testing.T) {
	_, err := Describe([]byte("select 'Doe, Jane"))
	var pe *judge.ParseError
	if !errors.As(err, &pe) || pe.Reason != finding.UnlexableSQL {
		t.Fatalf("err = %v, want unlexable-sql", err)
	}
	if strings.Contains(pe.Error(), "Doe") {
		t.Error("the error quotes the file")
	}
}

func TestDescribeNeverCarriesAValue(t *testing.T) {
	planted := []string{"Doe, Jane", "jane.doe@example.com", "212-555-0142", "2105-03-14"}
	d := describe(t, `-- owner: Doe, Jane
create view analytics."Doe, Jane" as
select * from warehouse."jane.doe@example.com"
where phone = '212-555-0142' and d = '2105-03-14' and n = 'Doe, Jane'
and x in (select * from {{ ref('212-555-0142') }})`)
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range planted {
		if strings.Contains(string(out), v) {
			t.Errorf("the description carries a planted value")
		}
	}
}

func TestNativeReadsLeavesOutCTEs(t *testing.T) {
	got, err := NativeReads(`with a as (select * from dbo.student) select * from a join term t on t.id = a.id`)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"dbo.student", "term"}) {
		t.Errorf("reads = %q", got)
	}
}

func TestDescribeTargetWithAColumnList(t *testing.T) {
	d := describe(t, "create table analytics.dim (id int);\n"+
		"insert into analytics.audit (id, note) select id, note from (select * from warehouse.dbo.term) x")
	if !slices.Equal(d.Creates, []Created{{Kind: "table", Name: "analytics.dim"}}) ||
		!slices.Equal(d.Writes, []string{"analytics.audit"}) ||
		!slices.Equal(d.Reads, []string{"warehouse.dbo.term"}) {
		t.Errorf("creates = %v, writes = %q, reads = %q", d.Creates, d.Writes, d.Reads)
	}
}
