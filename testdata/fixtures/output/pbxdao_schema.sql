;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

;

CREATE UNIQUE INDEX ON p_pbx_user  USING btree (msisdn , organisation_id ) ;

;

;

;

CREATE INDEX ON p_pbx_route  USING btree (organisation_id ) ;

;

;

;

CREATE INDEX ON p_pbx_route_schedule  USING btree (pbx_route_id ) ;

;

;

CREATE INDEX ON p_pbx_route_menu  USING btree (pbx_route_id ) ;

;

CREATE INDEX ON p_pbx_route_user  USING btree (pbx_route_id ) ;

;

;

;

;

;

;

;

;

CREATE UNIQUE INDEX ON p_pbx_customer  USING btree (organisation_id ) ;

;

;

;

;

;

;

;

;

