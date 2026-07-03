--
-- PostgreSQL database dump
--

\restrict a80qzMiKOGbUfXsw3MZodPW4BCZzScdDrDw5Qem1oQtfVNc21ifb6DkWgBpardZ

-- Dumped from database version 16.14 (Debian 16.14-1.pgdg13+1)
-- Dumped by pg_dump version 16.14 (Debian 16.14-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: widgets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.widgets (
    id integer NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: widgets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.widgets_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: widgets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.widgets_id_seq OWNED BY public.widgets.id;


--
-- Name: widgets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.widgets ALTER COLUMN id SET DEFAULT nextval('public.widgets_id_seq'::regclass);


--
-- Data for Name: widgets; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.widgets (id, name, created_at) FROM stdin;
1	alpha	2026-07-01 18:10:17.069646+00
2	bravo	2026-07-01 18:10:17.069646+00
3	charlie	2026-07-01 18:10:17.069646+00
\.


--
-- Name: widgets_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.widgets_id_seq', 3, true);


--
-- Name: widgets widgets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.widgets
    ADD CONSTRAINT widgets_pkey PRIMARY KEY (id);


--
-- PostgreSQL database dump complete
--

\unrestrict a80qzMiKOGbUfXsw3MZodPW4BCZzScdDrDw5Qem1oQtfVNc21ifb6DkWgBpardZ

