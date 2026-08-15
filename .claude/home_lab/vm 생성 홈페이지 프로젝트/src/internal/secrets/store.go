package secrets

import (
	"database/sql"

	"vm-portal/internal/models"
)

// DecryptedCredential is what phase-execution code needs: plaintext secrets
// held only in memory for the lifetime of a single job run.
type DecryptedCredential struct {
	models.Credential
	VCPassword   string
	ESXiPassword string
}

func SaveCredential(db *sql.DB, key []byte, name, vcenterIP, vcID, vcPassword, esxiPassword string, createdBy int64) (int64, error) {
	vcCipher, vcNonce, err := Encrypt(key, []byte(vcPassword))
	if err != nil {
		return 0, err
	}

	var esxiCipher, esxiNonce []byte
	if esxiPassword != "" {
		esxiCipher, esxiNonce, err = Encrypt(key, []byte(esxiPassword))
		if err != nil {
			return 0, err
		}
	}

	res, err := db.Exec(`
		INSERT INTO credentials (name, vcenter_ip, vc_id, encrypted_password, nonce, esxi_encrypted_password, esxi_nonce, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, vcenterIP, vcID, vcCipher, vcNonce, nullIfEmpty(esxiCipher), nullIfEmpty(esxiNonce), createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullIfEmpty(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

func ListCredentials(db *sql.DB) ([]models.Credential, error) {
	rows, err := db.Query(`SELECT id, name, vcenter_ip, vc_id, created_by, created_at FROM credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Credential
	for rows.Next() {
		var c models.Credential
		if err := rows.Scan(&c.ID, &c.Name, &c.VCenterIP, &c.VCID, &c.CreatedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetDecrypted loads a credential and decrypts its secrets with the master key.
func GetDecrypted(db *sql.DB, key []byte, id int64) (*DecryptedCredential, error) {
	row := db.QueryRow(`
		SELECT id, name, vcenter_ip, vc_id, encrypted_password, nonce, esxi_encrypted_password, esxi_nonce, created_by, created_at
		FROM credentials WHERE id = ?`, id)

	var c models.Credential
	var esxiCipher, esxiNonce []byte
	if err := row.Scan(&c.ID, &c.Name, &c.VCenterIP, &c.VCID, &c.EncryptedPassword, &c.Nonce, &esxiCipher, &esxiNonce, &c.CreatedBy, &c.CreatedAt); err != nil {
		return nil, err
	}

	vcPlain, err := Decrypt(key, c.EncryptedPassword, c.Nonce)
	if err != nil {
		return nil, err
	}

	dc := &DecryptedCredential{Credential: c, VCPassword: string(vcPlain)}
	if len(esxiCipher) > 0 {
		esxiPlain, err := Decrypt(key, esxiCipher, esxiNonce)
		if err != nil {
			return nil, err
		}
		dc.ESXiPassword = string(esxiPlain)
	}
	return dc, nil
}
