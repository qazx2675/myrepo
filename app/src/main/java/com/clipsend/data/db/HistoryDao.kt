package com.clipsend.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import androidx.room.Update
import kotlinx.coroutines.flow.Flow

@Dao
interface HistoryDao {

    @Insert
    suspend fun insert(entity: HistoryEntity): Long

    @Update
    suspend fun update(entity: HistoryEntity)

    @Query("SELECT * FROM send_history ORDER BY timestamp DESC")
    fun getAll(): Flow<List<HistoryEntity>>

    @Query(
        "SELECT * FROM send_history WHERE title LIKE '%' || :keyword || '%' " +
            "OR body LIKE '%' || :keyword || '%' ORDER BY timestamp DESC"
    )
    fun search(keyword: String): Flow<List<HistoryEntity>>

    @Query("SELECT * FROM send_history WHERE status = 'PENDING' ORDER BY timestamp ASC")
    suspend fun getPending(): List<HistoryEntity>
}