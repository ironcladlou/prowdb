insert or replace into jobs (
  id, name, result, started, duration, url, prowjob
) values (
  $id, $name, $result, $started, $duration, $url, $prowjob
);