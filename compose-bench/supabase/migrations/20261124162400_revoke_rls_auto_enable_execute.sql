-- Event trigger helper: should not be callable via the Data API.
revoke execute on function public.rls_auto_enable() from public, anon, authenticated;
