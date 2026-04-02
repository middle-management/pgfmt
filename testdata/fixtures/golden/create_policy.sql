-- Simple policy with USING and WITH CHECK
CREATE POLICY read_only
ON public.documents
TO authenticated
USING (true)
WITH CHECK (false);

-- Policy with FOR command
CREATE POLICY delete_own
ON public.documents
FOR DELETE
TO authenticated
USING (user_id = current_user_id());

-- Policy for ALL (default)
CREATE POLICY full_access
ON public.documents
TO admin
USING (true)
WITH CHECK (true);

-- Policy with SELECT command
CREATE POLICY select_active
ON public.documents
FOR SELECT
TO PUBLIC
USING (active = true);

-- Restrictive policy
CREATE POLICY restrict_ip
ON public.documents
AS RESTRICTIVE
TO authenticated
USING (client_ip = '10.0.0.1'::inet);

-- Policy with complex USING expression
CREATE POLICY row_security
ON public.orders
FOR SELECT
TO app_user
USING (EXISTS (
	SELECT
		1
	FROM
		public.user_roles AS ur
	WHERE
		(ur.user_id = orders.user_id)
		AND (ur.role = 'viewer')
));

-- Multiple roles
CREATE POLICY multi_role
ON public.documents
TO role1, role2, role3
USING (true);

-- Policy with INSERT and WITH CHECK only
CREATE POLICY insert_check
ON public.documents
FOR INSERT
TO writer
WITH CHECK (org_id = current_setting('app.org_id')::int);

