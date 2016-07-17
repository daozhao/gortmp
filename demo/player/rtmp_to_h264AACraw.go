package main

import (
    "encoding/binary"
    "encoding/hex"
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"github.com/zhangpeihao/goflv"
	rtmp "github.com/zhangpeihao/gortmp"
	//"github.com/zhangpeihao/log"
	"io"
	"net"
	"os"
	"time"
	"github.com/zhangpeihao/log"
)

const (
	programName = "RtmpPlayer"
	version     = "0.0.1"
)

var (
	url        *string = flag.String("URL", "rtmp://192.168.20.111/vid3", "The rtmp url to connect.")
	streamName *string = flag.String("Stream", "camstream", "Stream name to play.")
	dumpFlv    *string = flag.String("DumpFLV", "", "Dump FLV into file.")
	dumpAAC    *string = flag.String("DumpAAC", "", "Dump AAC into file.")
)

type TestOutboundConnHandler struct {
}

var obConn rtmp.OutboundConn
var createStreamChan chan rtmp.OutboundStream
var videoDataSize int64
var audioDataSize int64
var flvFile *flv.File
var h264RawFile *os.File
var AACRawFile *os.File

var status uint

func (handler *TestOutboundConnHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	status, err = conn.Status()
	fmt.Printf("@@@@@@@@@@@@@status: %d, err: %v\n", status, err)
}

func (handler *TestOutboundConnHandler) OnClosed(conn rtmp.Conn) {
	fmt.Printf("@@@@@@@@@@@@@Closed\n")
}

func (handler *TestOutboundConnHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
	switch message.Type {
	case rtmp.VIDEO_TYPE:
		if flvFile != nil {
			//flvFile.WriteVideoTag(message.Buf.Bytes(), message.Timestamp)
		}
        if h264RawFile != nil {
	        //message.Dump("h264dump")
            handler.OnVideo(message.Buf.Bytes(),message.Timestamp)
        }
		videoDataSize += int64(message.Buf.Len())
	case rtmp.AUDIO_TYPE:
		if AACRawFile != nil {
			handler.OnAudio(message.Buf.Bytes(),message.Timestamp)
			//flvFile.WriteAudioTag(message.Buf.Bytes(), message.Timestamp)
		}
		audioDataSize += int64(message.Buf.Len())
	}
}

var sfiv uint16
func (handler *TestOutboundConnHandler) OnAudio(data []byte, timestamp uint32) (err error) {
	// AAC而言加上界定符每一帧的前7字节是帧的描述信息
	// AAC 的贞分隔符是.0xFFF.比较怪,12个bit全1.不是字节对齐.
	// 另外贞头纪录贞的长度是13bit,真的怪,又是字节不对齐. 具体参照srs_aac_adts_frame_size
    //   sequence_heade的数据组成参考SrsRawAacStream::mux_sequence_header (rtmp to aac应该可以忽略这个包)
	// AAC的rtmp结构和 sequence_header的rtmp包结构 参照SrsRawAacStream::mux_aac2flv

	//  buf[0] = audio_header //audio_header的组成,参照SrsRawAacStream::mux_aac2flv
	//  buf[1] = aac_packet_type  //aac音频数据为 1  sequenc_header为 0
    //  buf[2-] = aac_raw_data  //aac原始数据. 不包含0xFFF等7byte的头部.

	var headbuf []byte = make([]byte,7)
	headbuf[0] = 0xff
	headbuf[1] = 0xf1

	//headbuf[2] = 0x5c
	//headbuf[3] = 0x80
	////3-5是长度计算,常见srs_aac_adts_frame_size函数.
	//headbuf[4] = 0x13
	//headbuf[5] = 0xa0
	//headbuf[6] = 0xfc
	if ( 1 == data[1] ) {
		//fmt.Printf("AAC data:\r\n%v\r\n",hex.Dump(data[0:15]))
		//fmt.Printf("\r\n%v\r\n",hex.Dump(data[2:17]))

		frameLength := uint16(len(data)-2+7)
		sfiv = sfiv | ( (frameLength  & 0x1800 ) >> 11)
		//headbuf[2] = (sfiv & 0x)
		binary.BigEndian.PutUint16(headbuf[2:],sfiv)


		var adfv uint32
		adfv = uint32(frameLength & 0x07ff) << 13

		headbuf[4] = uint8((adfv & 0x00FF0000 ) >> 16)
		headbuf[5] = uint8((adfv & 0x0000FF00 ) >> 8)
		//headbuf[6] = uint8((adfv & 0x000000FF ))
		headbuf[6] = 0xfc

		//fmt.Printf("AAC RAW data head(sfiv:0x%X frameLength:%d(0x%X)):\r\n%v\r\n.",
		//	sfiv,frameLength,frameLength,hex.Dump(headbuf))
		//fmt.Printf("AAC data:\r\n%v\r\n",hex.Dump(data[0:]))

		AACRawFile.Write(headbuf)
		AACRawFile.Write(data[2:])
	} else if ( 0 == data[1] ) {
		audioObjectType := (data[2+0] & 0xf8) >> 3
		var profile int8
		if ( 1 == audioObjectType ) {
            profile = 0 //SrsAacProfileMain
		} else if ( 2== audioObjectType ) {
			profile = 1 //SrsAacProfileLC
		} else if ( 3== audioObjectType ) {
			profile = 2 //SrsAacProfileSSR
		} else {
			fmt.Printf("Unknow AAC profile.")
		}

		samplingFrequencyIndex1 := (data[2+0] & 0x07) << 1
		samplingFrequencyIndex2 := (data[2+1] & 0x80) >> 7
		samplingFrequencyIndex := samplingFrequencyIndex1 | samplingFrequencyIndex2
		//var sound_rate int8
		//if ( 0x0a == samplingFrequencyIndex ){
		//	sound_rate = 1 //SrsCodecAudioSampleRate11025
		//} else if ( 0x07 == samplingFrequencyIndex ) {
		//	sound_rate = 2 //SrsCodecAudioSampleRate22050
		//} else if ( 0x04 == samplingFrequencyIndex ) {
		//	sound_rate = 3 //SrsCodecAudioSampleRate44100
		//} else {
		//	fmt.Printf("Unknow AAC sourd rate.")
		//}


		var channelConfiguration uint8
		channelConfiguration = (data[2+1] & 0x78) >> 3

		sfiv = 0
		sfiv = uint16(channelConfiguration & 0x07) << 6
		sfiv = sfiv | ( uint16(samplingFrequencyIndex & 0x0f) << 10)
		sfiv = sfiv | ( uint16(profile & 0x03) << 14)
		//sfiv = sfiv | ( (frameLength  & 0x1800 ) >> 11)


		fmt.Printf("AAC sequenc head data audioObjectType:0x%X samplingFrequencyIndex:0x%X  channelConfiguration:0x%X :\r\n%v",
			audioObjectType,samplingFrequencyIndex,channelConfiguration,hex.Dump(data[0:]))


	} else {

		fmt.Printf("AAC unknow data\r\n")
	}

	return  nil
}

func (handler *TestOutboundConnHandler) OnVideo(data []byte, timestamp uint32) (err error) {

	//这里处理接收到rtmp的video数据包,rtmp的数据包不只是包含h264的Raw数据,还有一些rmtp封装的包头.
	// 另外raw数据是不包括NAL的帧分隔符  00 00 01 或者 00 00 00 01
	//参考 http://www.codeman.net/2014/01/439.html 的代码示范,H.264关键帧 4.3 H.264非关键帧.但是代码写得不严谨

	// rtmp视频包在h264的包前边再添加了9个Byte.
	// 具体内容填写参考  SrsRawH264Stream::mux_ipb_frame 和  SrsRawH264Stream::mux_avc2flv
    //  这个是ipb贞的编码.
	// buf[0] = 贞类型和编码. ( (1|2)<<4 | 7 )  1是关键帧 2是P/B贞  7代表h264
	// buf[1] = 数据类型  ( 1 = NALU )
	// buf[2-4] = cts = pts - dts;
	// buf[5-8] = NAL的长度,不包括贞分隔符
	// buf[9-] = H264的RAW数据. 不包含分隔符,00 00 01 或者 00 00 00 01


    // sps和pps的贞另行处理.
	//  sps数据  nal_unit_type = 7的.  sps 长度 >0 < 4
	//  pps数据  nal_unit_type = 8的.  pps 长度 >0
	//  sps和pps数据直接在NAL中直接copy
	//  编码成rtmp包参考SrsRawH264Stream::mux_sequence_header
	//  rtmp sps pps sequence包结构
    // 推送的时候是这样,但是接收的时候有些字段不对.有可能服务器推送过来的字段有不同.

	// buf[0] = 贞类型和编码. ( (1<<4) | 7)   7代表h264
	// buf[1] = 数据类型 (0)
	// buf[2-4] = cts = pts - dts;
	//  buf[0+5] = 0x01
	//  buf[1+5] = sps[1]
	//  buf[2+5] = 0x00
	//  buf[3+5] = sps[3]
	//  buf[4+5] = 0x03

	//  buf[5+5] = 0x01
	//  buf[6-7(+5)] = sps.length
	//  buf[8-11(+5)] = sps   假设sps长度魏4.

	// buf[12(+5)] = 0x01
	// buf[13-14(+5)] = pps.length
	// buf[15-(+5)] = pps

	var headbuf []byte = make([]byte,4)

	headbuf[0] = 0x00
	headbuf[1] = 0x00
	headbuf[2] = 0x00
	headbuf[3] = 0x01

	var rawType int8
	var frameType int8
	var packetType int8

    rawType = int8(data[0]) & 0x0F   // h264 = 7
	frameType = (int8(data[0]) >> 4 ) & 0x0F   // 1= keyFrome(I贞) 2=interframe(P/B贞)  3= disposable_interframe  4= generated_keyframe 5=video_infoframe
	packetType = int8(data[1])   // sps_pps = 0  nalu = 1 sps_pps_end = 2

    //fmt.Printf("AAA Recevide Raw video rawType:%d frameType:%d pakcetType:%d len:%d\r\n",rawType,frameType,packetType,len(data))

    if ( 0x07 == rawType ) { //SrsCodecVideoAVC = 7
	    if ( 0 == packetType ) {
		    //sps_pps
            fmt.Printf("Recevide sps and pps\r\n%v\r\n",hex.Dump(data))
            //fmt.Printf("Recevide sps 0x%X 0x%X  \r\n",data[11],data[12])
            spsLength := binary.BigEndian.Uint16(data[11:])
            fmt.Printf("Recevide Raw video sps(%d)  \r\n",spsLength)
            ppsLength := binary.BigEndian.Uint16(data[14+spsLength:])
            fmt.Printf("Recevide Raw video sps(%d)  pps(%d) total len(%d) len-sps-pps=%d(=16)\r\n",spsLength,ppsLength,len(data),len(data)-int(spsLength+ppsLength) )
            //fmt.Printf("SPS:%v\r\n",data[13:13+spsLength])
            fmt.Printf("SPS:\r\n%v\r\n",hex.Dump(data[13:13+spsLength]))
            fmt.Printf("PPS:\r\n%v\r\n",hex.Dump(data[13+spsLength+3:]))
            h264RawFile.Write(headbuf)
            h264RawFile.Write(data[13:13+spsLength])
            h264RawFile.Write(headbuf)
            h264RawFile.Write(data[13+spsLength+3:])
	    } else if ( 1 == packetType ) {
            if ( 1 == frameType ) {
                fmt.Printf("Recevide Raw video rawType:%d frameType:%d pakcetType:%d len:%d\r\n", rawType, frameType, packetType, len(data))
            }
		    h264RawFile.Write(headbuf)
		    h264RawFile.Write(data[9:])
	    } else {
		    fmt.Printf("Recevide Raw video rawType:%d frameType:%d pakcetType:%d(unknow) len:%d\r\n",rawType,frameType,packetType,len(data))
	    }

        //fmt.Printf("Recevide SPS PPS video type:%X len:%d\r\n",data[0],len(data))
    } else {
	    fmt.Printf("Recevide Raw video rawType:%d frameType:%d pakcetType:%d len:%d\r\n",rawType,frameType,packetType,len(data))
        //fmt.Printf("Recevide Raw video type:%X len:%d\r\n",rawType,len(data))
        //h264RawFile.Write(headbuf)
        //h264RawFile.Write(data[9:])
    }

	return nil

}

func (handler *TestOutboundConnHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	fmt.Printf("ReceviedCommand: %+v\n", command)
}

func (handler *TestOutboundConnHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	fmt.Printf("Stream created: %d\n", stream.ID())
	createStreamChan <- stream
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	fmt.Printf("rtmp:%s stream:%s flv:%s aac:%s\r\n", *url,*streamName,*dumpFlv,*dumpAAC)
	//l := log.NewLogger(".", "player", nil, 60, 3600*24, true)
	l := log.NewStderrLogger()
	//l.SetMainLevel(log.LOG_LEVEL_DEBUG)
	var logHandler = l.LoggerModule("rtmp_h264")
	l.ModulePrintf(logHandler, log.LOG_LEVEL_DEBUG,"log is init:%s","abc")
	rtmp.InitLogger(l)
	defer l.Close()
	// Create flv file
	if len(*dumpFlv) > 0 {
		var err error
		//flvFile, err = flv.CreateFile(*dumpFlv)
		//if err != nil {
		//	fmt.Println("Create FLV dump file error:", err)
		//	return
		//}
		h264RawFile,err = os.Create(*dumpFlv)
		if err != nil {
			fmt.Println("Create h264Rawfile dump file error:", err)
			return
		}

	}
	if len(*dumpAAC) > 0 {
		var err error
		AACRawFile, err = os.Create(*dumpAAC)
		if err != nil {
			fmt.Println("Create h264Rawfile dump file error:", err)
			return
		}
	}
	defer func() {
		if h264RawFile != nil {
			h264RawFile.Close()
		}
		if AACRawFile != nil {
			AACRawFile.Close()
		}
		//if flvFile != nil {
		//	flvFile.Close()
		//}
	}()

	createStreamChan = make(chan rtmp.OutboundStream)
	testHandler := &TestOutboundConnHandler{}
	fmt.Println("to dial")

	var err error

	obConn, err = rtmp.Dial(*url, testHandler, 100)
	/*
		conn := TryHandshakeByVLC()
		obConn, err = rtmp.NewOutbounConn(conn, *url, testHandler, 100)
	*/
	if err != nil {
		fmt.Println("Dial error", err)
		os.Exit(-1)
	}

	defer obConn.Close()
	fmt.Printf("obConn: %+v\n", obConn)
	fmt.Printf("obConn.URL(): %s\n", obConn.URL())
	fmt.Println("to connect")
	//	err = obConn.Connect("33abf6e996f80e888b33ef0ea3a32bfd", "131228035", "161114738", "play", "", "", "1368083579")
	err = obConn.Connect()
	if err != nil {
		fmt.Printf("Connect error: %s", err.Error())
		os.Exit(-1)
	}
	for {
		select {
		case stream := <-createStreamChan:
			// Play
			err = stream.Play(*streamName, nil, nil, nil)
			if err != nil {
				fmt.Printf("Play error: %s", err.Error())
				os.Exit(-1)
			}
			// Set Buffer Length

		case <-time.After(1 * time.Second):
			fmt.Printf("Audio size: %d bytes; Video size: %d bytes\n", audioDataSize, videoDataSize)
		}
	}
}

////////////////////////////////////////////
func TryHandshakeByVLC() net.Conn {
	// listen
	listen, err := net.Listen("tcp", ":1935")
	if err != nil {
		fmt.Println("Listen error", err)
		os.Exit(-1)
	}
	defer listen.Close()

	iconn, err := listen.Accept()
	if err != nil {
		fmt.Println("Accept error", err)
		os.Exit(-1)
	}
	if iconn == nil {
		fmt.Println("iconn is nil")
		os.Exit(-1)
	}
	defer iconn.Close()
	// Handshake
	// C>>>P: C0+C1
	ibr := bufio.NewReader(iconn)
	ibw := bufio.NewWriter(iconn)
	c0, err := ibr.ReadByte()
	if c0 != 0x03 {
		fmt.Printf("C>>>P: C0(0x%2x) != 0x03\n", c0)
		os.Exit(-1)
	}
	c1 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(ibr, c1, rtmp.RTMP_SIG_SIZE)
	// Check C1
	var clientDigestOffset uint32
	if clientDigestOffset, err = CheckC1(c1, true); err != nil {
		fmt.Println("C>>>P: Test C1 err:", err)
		os.Exit(-1)
	}
	// P>>>S: Connect Server
	oconn, err := net.Dial("tcp", "192.168.20.111:1935")
	if err != nil {
		fmt.Println("P>>>S: Dial server err:", err)
		os.Exit(-1)
	}
	//	defer oconn.Close()
	obr := bufio.NewReader(oconn)
	obw := bufio.NewWriter(oconn)
	// P>>>S: C0+C1
	if err = obw.WriteByte(c0); err != nil {
		fmt.Println("P>>>S: Write C0 err:", err)
		os.Exit(-1)
	}
	if _, err = obw.Write(c1); err != nil {
		fmt.Println("P>>>S: Write C1 err:", err)
		os.Exit(-1)
	}
	if err = obw.Flush(); err != nil {
		fmt.Println("P>>>S: Flush err:", err)
		os.Exit(-1)
	}
	// P<<<S: Read S0+S1+S2
	s0, err := obr.ReadByte()
	if err != nil {
		fmt.Println("P<<<S: Read S0 err:", err)
		os.Exit(-1)
	}
	if c0 != 0x03 {
		fmt.Printf("P<<<S: S0(0x%2x) != 0x03\n", s0)
		os.Exit(-1)
	}
	s1 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(obr, s1, rtmp.RTMP_SIG_SIZE)
	if err != nil {
		fmt.Println("P<<<S: Read S1 err:", err)
		os.Exit(-1)
	}
	s2 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(obr, s2, rtmp.RTMP_SIG_SIZE)
	if err != nil {
		fmt.Println("P<<<S: Read S2 err:", err)
		os.Exit(-1)
	}

	// C<<<P: Send S0+S1+S2
	if err = ibw.WriteByte(s0); err != nil {
		fmt.Println("C<<<P: Write S0 err:", err)
		os.Exit(-1)
	}
	if _, err = ibw.Write(s1); err != nil {
		fmt.Println("C<<<P: Write S1 err:", err)
		os.Exit(-1)
	}
	if _, err = ibw.Write(s2); err != nil {
		fmt.Println("C<<<P: Write S2 err:", err)
		os.Exit(-1)
	}
	if err = ibw.Flush(); err != nil {
		fmt.Println("C<<<P: Flush err:", err)
		os.Exit(-1)
	}

	// C>>>P: Read C2
	c2 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(ibr, c2, rtmp.RTMP_SIG_SIZE)

	// Check S2
	server_pos := rtmp.ValidateDigest(s1, 8, rtmp.GENUINE_FP_KEY[:30])
	if server_pos == 0 {
		server_pos = rtmp.ValidateDigest(s1, 772, rtmp.GENUINE_FP_KEY[:30])
		if server_pos == 0 {
			fmt.Println("P<<<S: S1 position check error")
			os.Exit(-1)
		}
	}

	digest, err := rtmp.HMACsha256(c1[clientDigestOffset:clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FMS_KEY)
	rtmp.CheckError(err, "Get digest from c1 error")

	signature, err := rtmp.HMACsha256(s2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digest)
	rtmp.CheckError(err, "Get signature from s2 error")

	if bytes.Compare(signature, s2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		fmt.Println("Server signature mismatch")
		os.Exit(-1)
	}

	digestResp, err := rtmp.HMACsha256(s1[server_pos:server_pos+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FP_KEY)
	rtmp.CheckError(err, "Generate C2 HMACsha256 digestResp")
	signatureResp, err := rtmp.HMACsha256(c2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digestResp)
	if bytes.Compare(signatureResp, c2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		fmt.Println("C2 mismatch")
		os.Exit(-1)
	}

	// P>>>S: Send C2
	if _, err = obw.Write(c2); err != nil {
		fmt.Println("P>>>S: Write C2 err:", err)
		os.Exit(-1)
	}
	if err = obw.Flush(); err != nil {
		fmt.Println("P>>>S: Flush err:", err)
		os.Exit(-1)
	}
	return oconn
}
func CheckC1(c1 []byte, offset1 bool) (uint32, error) {
	var clientDigestOffset uint32
	if offset1 {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 8, 728, 12)
	} else {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 772, 728, 776)
	}
	// Create temp buffer
	tmpBuf := new(bytes.Buffer)
	tmpBuf.Write(c1[:clientDigestOffset])
	tmpBuf.Write(c1[clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH:])
	// Generate the hash
	tempHash, err := rtmp.HMACsha256(tmpBuf.Bytes(), rtmp.GENUINE_FP_KEY[:30])
	if err != nil {
		return 0, errors.New(fmt.Sprintf("HMACsha256 err: %s\n", err.Error()))
	}
	expect := c1[clientDigestOffset : clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH]
	if bytes.Compare(expect, tempHash) != 0 {
		return 0, errors.New(fmt.Sprintf("C1\nExpect % 2x\nGot    % 2x\n",
			expect,
			tempHash))
	}
	return clientDigestOffset, nil
}

func CheckC2(s1, c2 []byte) (uint32, error) {
	server_pos := rtmp.ValidateDigest(s1, 8, rtmp.GENUINE_FMS_KEY[:36])
	if server_pos == 0 {
		server_pos = rtmp.ValidateDigest(s1, 772, rtmp.GENUINE_FMS_KEY[:36])
		if server_pos == 0 {
			return 0, errors.New("Server response validating failed")
		}
	}

	digest, err := rtmp.HMACsha256(s1[server_pos:server_pos+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FP_KEY)
	rtmp.CheckError(err, "Get digest from s1 error")

	signature, err := rtmp.HMACsha256(c2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digest)
	rtmp.CheckError(err, "Get signature from c2 error")

	if bytes.Compare(signature, c2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		return 0, errors.New("Server signature mismatch")
	}
	return server_pos, nil
}
