-- Fix: reject_profile_identity_change() referenced NEW.<column> of other tables inside
-- one IF/ELSIF chain. PL/pgSQL resolves every field in an evaluated condition against
-- the firing table's row type, so ANY UPDATE on staff_profiles, doctor_profiles,
-- patients, specialties or rooms failed with SQLSTATE 42703. Nested IFs only touch
-- the columns of the table that fired the trigger. Behaviour is otherwise unchanged.
CREATE OR REPLACE FUNCTION reject_profile_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'staff_profiles' THEN
        IF NEW.user_id <> OLD.user_id OR NEW.staff_code <> OLD.staff_code THEN
            RAISE EXCEPTION 'staff profile identity is immutable' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'doctor_profiles' THEN
        IF NEW.staff_profile_id <> OLD.staff_profile_id OR NEW.doctor_code <> OLD.doctor_code THEN
            RAISE EXCEPTION 'doctor profile identity is immutable' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'patients' THEN
        IF NEW.user_id <> OLD.user_id OR NEW.patient_code <> OLD.patient_code THEN
            RAISE EXCEPTION 'patient identity is immutable' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'specialties' THEN
        IF NEW.code <> OLD.code THEN
            RAISE EXCEPTION 'specialty code is immutable' USING ERRCODE = '55000';
        END IF;
    ELSIF TG_TABLE_NAME = 'rooms' THEN
        IF NEW.code <> OLD.code THEN
            RAISE EXCEPTION 'room code is immutable' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
