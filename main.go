package main

import (
	crypto_rand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	math_rand "math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const TableSize = 4096
const RandDataSize = 4096
const StatRecordSize = 10

type RandDataProvider struct {
	mprngData       *math_rand.Rand
	mprngIndex      *math_rand.Rand
	tableUsage      [TableSize]int32
	tableData       [TableSize][]byte
	chanRefill      chan int
	refillThreshold int32
}

func (c *RandDataProvider) Close() error {
	close(c.chanRefill)
	return nil
}

func (c *RandDataProvider) shouldRefill(count int32) bool {
	return count > c.refillThreshold
}

func (c *RandDataProvider) UseRandData() []byte {
	idx := c.mprngIndex.Intn(TableSize)
	count := atomic.AddInt32(&c.tableUsage[idx], 1)
	if c.shouldRefill(count) {
		c.chanRefill <- idx
	}
	return c.tableData[idx]
}

func newRandDataProvider() *RandDataProvider {
	rdp := &RandDataProvider{}
	rdp.chanRefill = make(chan int, TableSize)
	bufMprngSeed := make([]byte, 8)

	seedMprng := func(mprng *math_rand.Rand) {
		_, err := io.ReadFull(crypto_rand.Reader, bufMprngSeed)
		if err != nil {
			log.Printf("can not read random data: %v", err)
		}
		seed := binary.LittleEndian.Uint64(bufMprngSeed)
		mprng.Seed(int64(seed))
	}

	fillRandData := func(i int) {
		seedMprng(rdp.mprngData)
		_, err := io.ReadFull(rdp.mprngData, rdp.tableData[i])
		if err != nil {
			log.Printf("can not read random data: %v", err)
		}
		rdp.tableUsage[i] = 0
	}

	refillLoop := func() {
		stateCount := 0
		for i := range rdp.chanRefill {
			if len(rdp.chanRefill) > TableSize/2 {
				stateCount++
				if stateCount > TableSize {
					stateCount = 0
					atomic.AddInt32(&rdp.refillThreshold, 1)
				}
			} else {
				stateCount++
				if stateCount > TableSize {
					stateCount = 0
					if rdp.refillThreshold > 0 {
						atomic.AddInt32(&rdp.refillThreshold, -1)
					}
				}
			}
			if rdp.shouldRefill(rdp.tableUsage[i]) {
				fillRandData(i)
			}
		}
	}

	rdp.mprngIndex = math_rand.New(math_rand.NewSource(0))
	seedMprng(rdp.mprngIndex)
	rdp.mprngData = math_rand.New(math_rand.NewSource(0))
	seedMprng(rdp.mprngData)

	for i := 0; i < TableSize; i++ {
		rdp.tableData[i] = make([]byte, RandDataSize)
		fillRandData(i)
	}

	go refillLoop()
	return rdp
}

func getDiskSpaceAvail(path string) uint64 {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return 0
	}
	return fs.Bavail * uint64(fs.Bsize)
}

func formatSize(size uint64) string {
	v := float64(size/1024) / 1024.0
	u := "MiB"
	if v > 1024 {
		v = v / 1024
		u = "GiB"
	}
	if v > 1024 {
		v = v / 1024
		u = "TiB"
	}
	if v > 1024 {
		v = v / 1024
		u = "PiB"
	}
	return fmt.Sprintf("%.2f %s", v, u)
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s {count} {file} [size]\n", os.Args[0])
		os.Exit(1)
	}

	fillCount, _ := strconv.Atoi(os.Args[1])
	if fillCount <= 0 {
		fmt.Printf("invalid count\n")
		os.Exit(1)
	}

	filePath := os.Args[2]
	fileDir := path.Dir(filePath)
	if st, err := os.Stat(fileDir); err != nil || !st.IsDir() {
		fmt.Printf("invalid parent directory for %s\n", filePath)
		os.Exit(1)
	}
	if _, err := os.Stat(filePath); err == nil || !os.IsNotExist(err) {
		fmt.Printf("invalid fill file, already exists: %s\n", filePath)
		os.Exit(1)
	}

	var fillSize uint64
	if len(os.Args) >= 4 {
		var err error
		fillSize, err = strconv.ParseUint(os.Args[3], 10, 64)
		if fillSize == 0 || err != nil {
			fmt.Printf("invalid fill size, err=%v\n", err)
			os.Exit(1)
		}
	}

	diskAvailSize := getDiskSpaceAvail(fileDir)
	log.Printf("fill %d times to file: %s, disk avail=%s", fillCount, filePath, formatSize(diskAvailSize))
	if fillSize > 0 {
		log.Printf("fill up to %s bytes", formatSize(fillSize))
	}

	type statRecord struct {
		recordTime   time.Time
		writtenBytes uint64
	}

	statRecords := make([]statRecord, 0)

	rdp := newRandDataProvider()
	for fillStep := 1; fillStep <= fillCount; fillStep++ {
		log.Printf("fill step %d, write to %s", fillStep, filePath)
		f, err := os.Create(filePath)
		if err != nil {
			log.Panicf("can not create file: %s, err=%v", filePath, err)
		}
		writtenTotal := uint64(0)
		statRecords = append(statRecords, statRecord{time.Now(), writtenTotal})
		loop := true
		for loop {
			d := rdp.UseRandData()

			var n int
			n, err = f.Write(d)
			if err != nil {
				log.Printf("fill step %d stops, err=%v", fillStep, err)
				break
			}

			writtenTotal += uint64(n)
			now := time.Now()
			dur := now.Sub(statRecords[len(statRecords)-1].recordTime)
			if dur > time.Second {
				statRecords = append(statRecords, statRecord{now, writtenTotal})
				speed := float64(writtenTotal-statRecords[len(statRecords)-2].writtenBytes) / dur.Seconds()
				msg := fmt.Sprintf("step: %d, speed: %s/s (written: %s) ...", fillStep, formatSize(uint64(speed)), formatSize(writtenTotal))
				if len(statRecords) >= StatRecordSize {
					durAvg := now.Sub(statRecords[0].recordTime)
					speedAvg := float64(writtenTotal-statRecords[0].writtenBytes) / durAvg.Seconds()
					var remainingSize uint64
					var remainingRatio float64
					if fillSize > 0 {
						remainingSize = fillSize - writtenTotal
						remainingRatio = float64(remainingSize) / float64(fillSize)
					} else if diskAvailSize > 0 {
						remainingSize = diskAvailSize - writtenTotal
						remainingRatio = float64(remainingSize) / float64(diskAvailSize)
					}
					if speedAvg > 0 {
						msg = msg + ", estimated time: " + (time.Duration(remainingSize/uint64(speedAvg)) * time.Second).String()
					} else {
						msg = msg + ", no speed"
					}
					if speedAvg < 50*1024 && remainingRatio < 0.001 {
						msg = msg + ", nearly fill to be written in"
						loop = false
					}
					statRecords = statRecords[1:]
				}
				print("\033[2K\r", msg)
			}
			if fillSize > 0 {
				if writtenTotal >= fillSize {
					loop = false
				}
			}
		}
		print("\n")
		_ = f.Close()
		filePathAbs, _ := filepath.Abs(filePath)
		log.Printf("fill step %d written %s bytes to %s", fillStep, formatSize(writtenTotal), filePathAbs)

		if fillStep != fillCount {
			log.Printf("fill step %d removes file %s", fillStep, filePathAbs)
			err = os.Remove(filePath)
			if err != nil {
				log.Panicf("can not remove file: %s, err=%v", filePath, err)
			}
		} else {
			log.Printf("fill step %d (final) keeps file %s", fillStep, filePathAbs)
		}
	}
	_ = rdp.Close()
}
