begin;

-- Create a reusable function for updating the 'updated_at' column
create or replace function public.set_updated_at()
returns trigger as $$
begin
  new.updated_at = now();
  return new;
end;
$$ language plpgsql;

create table if not exists public.sensor_data (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

alter table public.sensor_data
enable row level security;

create index if not exists sensor_data_updated_at_idx
  on public.sensor_data (updated_at);

--  Create Trigger for 'sensor_data'
create or replace trigger trg_sensor_data_updated_at
  before update on public.sensor_data
  for each row
  execute function public.set_updated_at();

create table if not exists public.use_case (
  sensor_id bigint primary key,
  message jsonb not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

alter table public.use_case
enable row level security;

create index if not exists use_case_updated_at_idx
  on public.use_case (updated_at);

create or replace trigger trg_use_case_updated_at
  before update on public.use_case
  for each row
  execute function public.set_updated_at();

commit;
