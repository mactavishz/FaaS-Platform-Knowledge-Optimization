begin;

-- Create a reusable function for updating the 'updated_at' column
create or replace function public.set_updated_at()
returns trigger as $$
begin
  new.updated_at = now();
  return new;
end;
$$ language plpgsql;

 create table if not exists public.:"sensor_data_table" (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

alter table public.:"sensor_data_table"
enable row level security;

create index if not exists :"sensor_data_updated_at_idx"
  on public.:"sensor_data_table" (updated_at);

--  Create Trigger for 'sensor_data'
drop trigger if exists :"sensor_data_updated_at_trigger" on public.:"sensor_data_table";

create trigger :"sensor_data_updated_at_trigger"
  before update on public.:"sensor_data_table"
  for each row
  execute function public.set_updated_at();

create table if not exists public.:"use_case_table" (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

alter table public.:"use_case_table"
enable row level security;

create index if not exists :"use_case_updated_at_idx"
  on public.:"use_case_table" (updated_at);

drop trigger if exists :"use_case_updated_at_trigger" on public.:"use_case_table";

create trigger :"use_case_updated_at_trigger"
  before update on public.:"use_case_table"
  for each row
  execute function public.set_updated_at();

commit;
