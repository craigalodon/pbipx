select section_number, count(*) as students
from enrollment
where cohort_year = 2101
group by section_number;
