create table if not exists jobs (
  id text not null primary key,
  name text,
  result text,
  started text,
  duration numeric,
  url text,
  prowjob text
);